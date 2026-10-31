package semantic

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Two replicas over one pgvector table: B opened before A wrote, the
// way every replica of a running deployment has. With a MemoryKeyword
// each, B's keyword leg never saw A's document until B restarted. The
// store's own keyword index reads the table, so B finds it at once.
func TestPgReplicasShareKeywordLeg(t *testing.T) {
	emb := NewStubEmbedder(8)
	dbA := openPgVector(t)
	dbB := pgtest.Replica(t, dbA)
	mk := func(db *sql.DB) *PgVectorStore {
		s, err := NewPgVector(db, PgVectorConfig{Dim: emb.Dim()})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EnsureSchema(context.Background()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	sa, sb := mk(dbA), mk(dbB)
	open := func(s *PgVectorStore) Index {
		idx, err := Open(Options{Embedder: emb, Store: s, Keyword: s.Keyword()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { idx.Close() })
		return idx
	}
	idxA, idxB := open(sa), open(sb)
	ctx := context.Background()
	if err := idxA.Add(ctx, Document{ID: "a", Text: "quokka husbandry guidelines"}); err != nil {
		t.Fatal(err)
	}
	hits, err := sb.Keyword().Search(ctx, "Quokka", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("replica B keyword hits = %v, want A's chunk", hits)
	}
	if c, ok := sb.ChunkByID(hits[0].ChunkID); !ok || c.DocID != "a" {
		t.Fatalf("replica B keyword hit %q is not doc a's chunk", hits[0].ChunkID)
	}
	got, err := idxB.Query(ctx, Query{Text: "quokka", K: 5, Hybrid: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Chunk.DocID != "a" {
		t.Fatalf("replica B hybrid hits = %v, want doc a", hitIDs(got))
	}

	// A removal through A leaves B's keyword leg too.
	if err := idxA.Remove(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if hits, err := sb.Keyword().Search(ctx, "quokka", 5); err != nil || len(hits) != 0 {
		t.Fatalf("after remove, replica B keyword hits = %v, %v", hits, err)
	}
}

// The store's keyword index matches any query term, ranks a row with
// more of them first, and answers nothing for a query with no terms.
func TestPgKeywordSearchRanks(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "alpha", DocID: "d1", Text: "zebra grazing", Vec: vec3(1, 0, 0)},
		{ID: "both", DocID: "d2", Text: "zebra ledger, zebra crossing", Vec: vec3(0, 1, 0)},
		{ID: "none", DocID: "d3", Text: "giraffe", Vec: vec3(0, 0, 1)},
	}); err != nil {
		t.Fatal(err)
	}
	kw := s.Keyword()
	hits, err := kw.Search(ctx, "ledger zebra zebra", 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, h := range hits {
		ids = append(ids, h.ChunkID)
	}
	if !slices.Equal(ids, []string{"both", "alpha"}) {
		t.Fatalf("hits = %v, want [both alpha]", ids)
	}
	if hits, err := kw.Search(ctx, "ledger zebra", 1); err != nil || len(hits) != 1 {
		t.Fatalf("top 1 = %v, %v", hits, err)
	}
	if hits, err := kw.Search(ctx, "zebra", 0); err != nil || len(hits) != 2 {
		t.Fatalf("top 0 (the default) = %v, %v", hits, err)
	}
	// Quote and tsquery operator characters are not terms.
	for _, q := range []string{"", "!!! ' | & :* <->", "o'"} {
		hits, err := kw.Search(ctx, q, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(hits) != 0 {
			t.Fatalf("Search(%q) = %v, want none", q, hits)
		}
	}
}

// A MemoryKeyword lives in one process; a PgVectorStore is shared by
// every replica. Open refuses the pair instead of serving a keyword leg
// that misses the other replicas' writes, and refuses one store's
// keyword index paired with another store, whose rows it would not
// hydrate.
func TestOpenRefusesLocalKeywordOnSharedStore(t *testing.T) {
	emb := NewStubEmbedder(3)
	s := newPgStore(t, 3)
	_, err := Open(Options{Embedder: emb, Store: s, Keyword: NewMemoryKeyword()})
	if err == nil || !strings.Contains(err.Error(), "Keyword()") {
		t.Fatalf("Open(MemoryKeyword, PgVectorStore) err = %v, want a refusal naming Keyword()", err)
	}
	other, err := NewPgVector(s.db, PgVectorConfig{Dim: 3, Table: "other_chunks"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(Options{Embedder: emb, Store: s, Keyword: other.Keyword()})
	if err == nil || !strings.Contains(err.Error(), "another store") {
		t.Fatalf("Open(other store's keyword) err = %v, want a refusal", err)
	}
	idx, err := Open(Options{Embedder: emb, Store: s, Keyword: s.Keyword()})
	if err != nil {
		t.Fatalf("Open(own keyword): %v", err)
	}
	idx.Close()
	// A FlatStore is one process's own; MemoryKeyword stays right there.
	idx, err = Open(Options{Embedder: emb, Keyword: NewMemoryKeyword()})
	if err != nil {
		t.Fatalf("Open(MemoryKeyword, FlatStore): %v", err)
	}
	idx.Close()
}

// The store's keyword index needs no rebuild, so Open reads no rows:
// a table that cannot be read fails at query time, not at Open, and a
// large table is not loaded into memory on every start.
func TestPgKeywordOpenReadsNoRows(t *testing.T) {
	s := newPgStore(t, 3)
	if _, err := s.db.Exec(`ALTER TABLE embed_chunks RENAME COLUMN text TO body`); err != nil {
		t.Fatal(err)
	}
	idx, err := Open(Options{Embedder: NewStubEmbedder(3), Store: s, Keyword: s.Keyword()})
	if err != nil {
		t.Fatalf("Open read the table: %v", err)
	}
	idx.Close()
}
