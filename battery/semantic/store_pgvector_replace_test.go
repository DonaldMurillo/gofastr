package semantic

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// docTexts returns the stored chunk texts of docID, in chunk-ID order.
func docTexts(t *testing.T, s *PgVectorStore, docID string) []string {
	t.Helper()
	var out []string
	for _, id := range s.ChunkIDsForDoc(docID) {
		c, ok := s.ChunkByID(id)
		if !ok {
			t.Fatalf("chunk %q listed but not readable", id)
		}
		out = append(out, c.Text)
	}
	slices.Sort(out)
	return out
}

func chunkTexts(t *testing.T, ch Chunker, doc Document) []string {
	t.Helper()
	cs, err := ch.Chunk(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Text)
	}
	slices.Sort(out)
	return out
}

// A replace whose new version cannot be stored keeps the old version:
// the delete and the inserts are one transaction.
func TestPgReplaceFailKeepsOldDoc(t *testing.T) {
	cases := map[string]Document{
		"nan metadata": {ID: "d", Text: "new body", Metadata: map[string]any{"x": math.NaN()}},
		"insert error": {ID: "d", Text: "new\x00body"},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			emb := NewStubEmbedder(8)
			store := newPgStore(t, emb.Dim())
			kw := NewMemoryKeyword()
			idx, err := Open(Options{Embedder: emb, Store: store, Keyword: kw})
			if err != nil {
				t.Fatal(err)
			}
			defer idx.Close()
			ctx := context.Background()
			if err := idx.Add(ctx, Document{ID: "d", Text: "old zebra body"}); err != nil {
				t.Fatal(err)
			}
			if err := idx.Add(ctx, bad); err == nil {
				t.Fatal("replace with an unstorable version succeeded")
			}
			if got := docTexts(t, store, "d"); !slices.Equal(got, []string{"old zebra body"}) {
				t.Fatalf("after failed replace doc d = %q, want the old version", got)
			}
			hits, err := kw.Search(ctx, "zebra", 5)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 {
				t.Fatalf("keyword hits for the old version = %v, want 1", hits)
			}
		})
	}
}

// Two replicas replacing one document over a shared table leave one whole
// version. Replica A pauses after its delete; replica B replaces the same
// document meanwhile. Without a cross-process lock the two deletes run
// before both inserts and the table keeps chunks of both versions.
func TestPgReplaceSerializesReplicas(t *testing.T) {
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
	ch := NewFixedWindow(8, 0)
	open := func(s *PgVectorStore) Index {
		idx, err := Open(Options{Embedder: emb, Store: s, Chunker: ch})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { idx.Close() })
		return idx
	}
	idxA, idxB := open(sa), open(sb)
	ctx := context.Background()
	v0 := Document{ID: "doc", Text: "seed"}
	if err := idxA.Add(ctx, v0); err != nil {
		t.Fatal(err)
	}

	bDeleted := make(chan struct{})
	var bOnce sync.Once
	sb.afterDelete = func() { bOnce.Do(func() { close(bDeleted) }) }
	aPaused := make(chan struct{})
	var aOnce sync.Once
	sa.afterDelete = func() {
		aOnce.Do(func() {
			close(aPaused)
			select {
			case <-bDeleted:
			case <-time.After(time.Second):
			}
		})
	}

	vA := Document{ID: "doc", Text: "alpha one alpha two alpha three"}
	vB := Document{ID: "doc", Text: "beta"}
	var wg sync.WaitGroup
	var errA, errB error
	wg.Add(2)
	go func() { defer wg.Done(); errA = idxA.Add(ctx, vA) }()
	go func() {
		defer wg.Done()
		<-aPaused
		errB = idxB.Add(ctx, vB)
	}()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("replace errors: A=%v B=%v", errA, errB)
	}
	got := docTexts(t, sa, "doc")
	if !slices.Equal(got, chunkTexts(t, ch, vA)) && !slices.Equal(got, chunkTexts(t, ch, vB)) {
		t.Fatalf("doc holds %q: a mix of versions, want exactly one", got)
	}
}
