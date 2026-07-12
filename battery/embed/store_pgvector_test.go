package embed

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// pgvector integration tests. A live Postgres-with-pgvector comes from
// $TEST_POSTGRES_DSN if set, otherwise an ephemeral testcontainer built from
// pgvector/pgvector:pg16 (which ships the vector type). If neither is
// reachable the suite skips — it never fails for lack of a database, the same
// convention as battery/search/postgres_test.go.
//
// Each test gets its own throwaway schema on the shared instance so they run
// in parallel without colliding. SetMaxOpenConns(1) keeps the per-connection
// search_path stable so the bare table name resolves into the test schema.

var (
	pgVecOnce    sync.Once
	pgVecBaseDSN string
	pgVecErr     error
	pgVecUsing   string
	pgVecLogged  atomic.Bool
	pgVecKeepRef *tcpostgres.PostgresContainer
)

func resolvePgVector() (string, error) {
	pgVecOnce.Do(func() {
		if dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")); dsn != "" {
			pgVecBaseDSN = dsn
			pgVecUsing = "env"
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		// pgvector/pgvector:pg16 bundles the vector extension; the plain
		// postgres:16-alpine image used by battery/search has no vector type.
		c, err := tcpostgres.Run(ctx, "pgvector/pgvector:pg16",
			tcpostgres.WithDatabase("embed_test"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
		)
		if err != nil {
			pgVecErr = fmt.Errorf("testcontainers: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgVecErr = err
			return
		}
		pgVecBaseDSN = dsn
		pgVecUsing = "container"
		pgVecKeepRef = c
	})
	return pgVecBaseDSN, pgVecErr
}

// openPgVector returns a *sql.DB bound to a fresh isolated schema.
func openPgVector(t *testing.T) *sql.DB {
	t.Helper()
	dsn, err := resolvePgVector()
	if err != nil {
		t.Skipf("pgvector unavailable: %v", err)
	}
	if !pgVecLogged.Swap(true) {
		t.Logf("battery/embed pgvector tests using %s", pgVecUsing)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1)
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		if err := db.PingContext(ctx); err == nil {
			cancel()
			break
		}
		cancel()
		time.Sleep(200 * time.Millisecond)
	}
	schema := fmt.Sprintf("embed_%d", time.Now().UnixNano())
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP SCHEMA " + schema + " CASCADE")
		db.Close()
	})
	return db
}

// newPgStore builds a PgVectorStore on a fresh schema and ensures the schema
// exists.
func newPgStore(t *testing.T, dim int) *PgVectorStore {
	t.Helper()
	db := openPgVector(t)
	s, err := NewPgVector(db, PgVectorConfig{Dim: dim, Model: "test"})
	if err != nil {
		t.Fatalf("NewPgVector: %v", err)
	}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s
}

// vec3 is a small already-normalized 3-dim vector helper for the ranking
// tests. The caller controls exact geometry so parity is deterministic.
func vec3(a, b, c float32) []float32 { return []float32{a, b, c} }

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func TestPgVectorAddCandidates(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	chunks := []Chunk{
		{ID: "a", DocID: "d1", Text: "alpha", Vec: vec3(1, 0, 0)},
		{ID: "b", DocID: "d2", Text: "beta", Vec: vec3(0, 1, 0)},
		{ID: "c", DocID: "d3", Text: "gamma", Vec: vec3(0.70710677, 0.70710677, 0)},
	}
	if err := s.Add(ctx, chunks); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{}, 10)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	wantOrder := []string{"a", "c", "b"}
	if len(hits) != len(wantOrder) {
		t.Fatalf("got %d hits, want %d", len(hits), len(wantOrder))
	}
	for i, h := range hits {
		if h.Chunk.ID != wantOrder[i] {
			t.Fatalf("hit %d = %q, want %q (full order: %v)", i, h.Chunk.ID, wantOrder[i], hitIDs(hits))
		}
	}
	// Nearest must be a perfect cosine of 1.0.
	if !approxEqual(hits[0].Score, 1.0) {
		t.Errorf("top score = %v, want 1.0", hits[0].Score)
	}
	// Returned candidates must carry Vec for downstream MMR.
	if len(hits[0].Chunk.Vec) != 3 {
		t.Errorf("top hit Vec len = %d, want 3", len(hits[0].Chunk.Vec))
	}
	if hits[0].Reason != "vec" {
		t.Errorf("Reason = %q, want vec", hits[0].Reason)
	}
}

// TestPgVectorRankingParity is the key correctness test: given identical
// normalized vectors, PgVectorStore and FlatStore must produce the SAME top-K
// order (RRF fusion depends only on rank order, not score magnitude).
func TestPgVectorRankingParity(t *testing.T) {
	ctx := context.Background()
	dim := 3
	flat := NewFlatStore(dim, "test")
	pg := newPgStore(t, dim)

	chunks := []Chunk{
		{ID: "k1", DocID: "doc", Text: "one", Vec: vec3(1, 0, 0)},
		{ID: "k2", DocID: "doc", Text: "two", Vec: vec3(0, 1, 0)},
		{ID: "k3", DocID: "doc", Text: "three", Vec: vec3(0.70710677, 0.70710677, 0)},
		{ID: "k4", DocID: "doc", Text: "four", Vec: vec3(0.6, 0.8, 0)},
		{ID: "k5", DocID: "doc", Text: "five", Vec: vec3(0, 0, 1)},
	}
	if err := flat.Add(ctx, cloneChunks(chunks)); err != nil {
		t.Fatalf("flat Add: %v", err)
	}
	if err := pg.Add(ctx, cloneChunks(chunks)); err != nil {
		t.Fatalf("pg Add: %v", err)
	}
	qv := vec3(0.9, 0.4, 0.05) // biased toward k4 (0.6,0.8) and k1 (1,0)
	flatHits, err := flat.Candidates(ctx, qv, Filter{}, 10)
	if err != nil {
		t.Fatalf("flat Candidates: %v", err)
	}
	pgHits, err := pg.Candidates(ctx, qv, Filter{}, 10)
	if err != nil {
		t.Fatalf("pg Candidates: %v", err)
	}
	if len(flatHits) != len(pgHits) {
		t.Fatalf("count mismatch: flat=%d pg=%d", len(flatHits), len(pgHits))
	}
	for i := range flatHits {
		if flatHits[i].Chunk.ID != pgHits[i].Chunk.ID {
			t.Fatalf("rank %d: flat=%q pg=%q\nflat order: %v\npg   order: %v",
				i, flatHits[i].Chunk.ID, pgHits[i].Chunk.ID,
				hitIDs(flatHits), hitIDs(pgHits))
		}
		if !approxEqual(flatHits[i].Score, pgHits[i].Score) {
			t.Fatalf("rank %d (%q): flat score=%v pg score=%v",
				i, flatHits[i].Chunk.ID, flatHits[i].Score, pgHits[i].Score)
		}
	}
}

func TestPgVectorRemoveDoc(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a1", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0)},
		{ID: "a2", DocID: "d1", Text: "y", Vec: vec3(0, 1, 0)},
		{ID: "b1", DocID: "d2", Text: "z", Vec: vec3(0, 0, 1)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := s.Stats().Chunks; got != 3 {
		t.Fatalf("before remove: chunks=%d, want 3", got)
	}
	if err := s.RemoveDoc(ctx, "d1"); err != nil {
		t.Fatalf("RemoveDoc: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{}, 10)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "b1" {
		t.Fatalf("after remove d1: got %v, want [b1]", hitIDs(hits))
	}
	if got := s.Stats().Docs; got != 1 {
		t.Errorf("after remove: docs=%d, want 1", got)
	}
}

func TestPgVectorFilterSource(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Source: "api", Text: "x", Vec: vec3(1, 0, 0)},
		{ID: "b", DocID: "d2", Source: "web", Text: "y", Vec: vec3(0.9, 0.43, 0)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{Source: "api"}, 10)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "a" {
		t.Fatalf("filter Source=api: got %v, want [a]", hitIDs(hits))
	}
}

func TestPgVectorFilterKind(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0), Metadata: map[string]any{"kind": "guide"}},
		{ID: "b", DocID: "d2", Text: "y", Vec: vec3(0.9, 0.43, 0), Metadata: map[string]any{"kind": "api"}},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{Kind: "guide"}, 10)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "a" {
		t.Fatalf("filter Kind=guide: got %v, want [a]", hitIDs(hits))
	}
}

func TestPgVectorFilterMetaMatch(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0), Metadata: map[string]any{"tenant": "acme"}},
		{ID: "b", DocID: "d2", Text: "y", Vec: vec3(0.9, 0.43, 0), Metadata: map[string]any{"tenant": "globex"}},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{MetaMatch: map[string]any{"tenant": "globex"}}, 10)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "b" {
		t.Fatalf("filter MetaMatch tenant=globex: got %v, want [b]", hitIDs(hits))
	}
}

func TestPgVectorChunkListerRoundTrip(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a1", DocID: "d1", Source: "s", Text: "alpha", Offset: [2]int{0, 5}, Vec: vec3(1, 0, 0), Metadata: map[string]any{"k": "v"}},
		{ID: "a2", DocID: "d1", Source: "s", Text: "beta", Offset: [2]int{6, 10}, Vec: vec3(0, 1, 0)},
		{ID: "b1", DocID: "d2", Source: "s", Text: "gamma", Offset: [2]int{0, 5}, Vec: vec3(0, 0, 1)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// ChunkIDsForDoc
	ids := s.ChunkIDsForDoc("d1")
	if len(ids) != 2 || ids[0] != "a1" || ids[1] != "a2" {
		t.Fatalf("ChunkIDsForDoc(d1)=%v, want [a1 a2]", ids)
	}
	// ChunkByID round-trips all fields incl. offset + metadata + vec.
	c, ok := s.ChunkByID("a1")
	if !ok {
		t.Fatal("ChunkByID(a1) not found")
	}
	if c.DocID != "d1" || c.Text != "alpha" || c.Source != "s" {
		t.Errorf("ChunkByID fields mismatch: %+v", c)
	}
	if c.Offset != [2]int{0, 5} {
		t.Errorf("Offset = %v, want [0 5]", c.Offset)
	}
	if got := c.Metadata["k"]; got != "v" {
		t.Errorf("Metadata[k] = %v, want v", got)
	}
	if len(c.Vec) != 3 || math.Abs(float64(c.Vec[0])-1) > 1e-4 {
		t.Errorf("Vec = %v, want ~[1 0 0]", c.Vec)
	}
	if _, ok := s.ChunkByID("nope"); ok {
		t.Error("ChunkByID(nope) should be missing")
	}
	// AllChunks
	all := s.AllChunks()
	if len(all) != 3 {
		t.Fatalf("AllChunks len = %d, want 3", len(all))
	}
}

func TestPgVectorStats(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "a1", DocID: "d1", Text: "x", Vec: vec3(1, 0, 0)},
		{ID: "a2", DocID: "d1", Text: "y", Vec: vec3(0, 1, 0)},
		{ID: "b1", DocID: "d2", Text: "z", Vec: vec3(0, 0, 1)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	st := s.Stats()
	if st.Docs != 2 {
		t.Errorf("Docs = %d, want 2", st.Docs)
	}
	if st.Chunks != 3 {
		t.Errorf("Chunks = %d, want 3", st.Chunks)
	}
	if st.Dim != 3 {
		t.Errorf("Dim = %d, want 3", st.Dim)
	}
	if st.Model != "test" {
		t.Errorf("Model = %q, want test", st.Model)
	}
}

func TestPgVectorEnsureSchemaIdempotent(t *testing.T) {
	s := newPgStore(t, 3)
	// newPgStore already called EnsureSchema once; calling again must be safe.
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("third EnsureSchema: %v", err)
	}
}

// TestPgVectorHybridEndToEnd proves the store composes through embed.Open with
// a keyword backend (chunkLister is what makes hydration work).
func TestPgVectorHybridEndToEnd(t *testing.T) {
	dim := 16
	emb := NewStubEmbedder(dim)
	db := openPgVector(t)
	store, err := NewPgVector(db, PgVectorConfig{Dim: emb.Dim()})
	if err != nil {
		t.Fatalf("NewPgVector: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	idx, err := Open(Options{
		Embedder: emb,
		Store:    store,
		Keyword:  NewMemoryKeyword(),
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer idx.Close()
	ctx := context.Background()
	if err := idx.Add(ctx,
		Document{ID: "session", Source: "auth.md", Text: "how session middleware validates tokens"},
		Document{ID: "router", Source: "core.md", Text: "the router dispatches http handlers by path"},
		Document{ID: "cache", Source: "cache.md", Text: "in-memory cache eviction policies"},
	); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := idx.Query(ctx, Query{Text: "session token validation", K: 3, Hybrid: true, MMRLambda: 0.3})
	if err != nil {
		t.Fatalf("Query hybrid: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid query returned no hits")
	}
	// Vectors are stripped on the way out.
	if len(hits[0].Chunk.Vec) != 0 {
		t.Errorf("returned hit still carries Vec (len %d)", len(hits[0].Chunk.Vec))
	}
}

// TestPgVectorOpenRejectsPath confirms the fail-closed behaviour: a durable
// store paired with Options.Path must error at Open (no snapshotter cap).
func TestPgVectorOpenRejectsPath(t *testing.T) {
	emb := NewStubEmbedder(8)
	db := openPgVector(t)
	store, err := NewPgVector(db, PgVectorConfig{Dim: emb.Dim()})
	if err != nil {
		t.Fatalf("NewPgVector: %v", err)
	}
	_, err = Open(Options{Embedder: emb, Store: store, Path: "/tmp/embed-nope"})
	if err == nil {
		t.Fatal("Open with Path + PgVectorStore should error (no snapshotter)")
	}
	if !strings.Contains(err.Error(), "persist") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- helpers ---

func hitIDs(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Chunk.ID
	}
	return out
}

func cloneChunks(in []Chunk) []Chunk {
	out := make([]Chunk, len(in))
	for i := range in {
		out[i] = in[i]
		v := make([]float32, len(in[i].Vec))
		copy(v, in[i].Vec)
		out[i].Vec = v
	}
	return out
}
