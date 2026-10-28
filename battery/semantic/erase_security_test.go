package semantic

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/framework"
)

// docOwners returns "doc_id:owner" for each stored chunk's document,
// sorted and deduplicated.
func docOwners(t *testing.T, idx Index) []string {
	t.Helper()
	hits, err := idx.Query(context.Background(), Query{Text: "diary", K: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var got []string
	for _, h := range hits {
		got = append(got, h.Chunk.DocID+":"+h.Chunk.Owner)
	}
	slices.Sort(got)
	return slices.Compact(got)
}

// Erasing a user removes the chunks indexed from their documents and
// keeps everyone else's (S24). The owner rides from Document to every
// chunk, whatever the chunker returned.
func TestEraseRemovesPgVectorChunks(t *testing.T) {
	db := openPgVector(t)
	store, err := NewPgVector(db, PgVectorConfig{Dim: 8, Model: "test", Table: "note_chunks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	idx, err := Open(Options{Embedder: NewStubEmbedder(8), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	ctx := context.Background()
	if err := idx.Add(ctx,
		Document{ID: "v1", Text: "diary of the victim", Owner: "victim"},
		Document{ID: "b1", Text: "diary of the bystander", Owner: "bystander"},
		Document{ID: "n1", Text: "diary help page"},
	); err != nil {
		t.Fatal(err)
	}
	if got, want := docOwners(t, idx), []string{"b1:bystander", "n1:", "v1:victim"}; !slices.Equal(got, want) {
		t.Fatalf("before erase: %v, want %v", got, want)
	}
	if _, err := framework.NewApp(framework.WithDB(db)).EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if got, want := docOwners(t, idx), []string{"b1:bystander", "n1:"}; !slices.Equal(got, want) {
		t.Errorf("after erase: %v, want %v", got, want)
	}
}

// A chunk table an earlier release created gains the owner column.
func TestPgVectorAddsOwnerColumn(t *testing.T) {
	db := openPgVector(t)
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("pgvector unavailable: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE embed_chunks (id TEXT PRIMARY KEY, doc_id TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '',
		offset_start INTEGER NOT NULL DEFAULT 0, offset_end INTEGER NOT NULL DEFAULT 0,
		meta JSONB NOT NULL DEFAULT '{}', embedding vector(8) NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	store, err := NewPgVector(db, PgVectorConfig{Dim: 8, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema over an old table: %v", err)
		}
	}
	if _, err := db.Exec(`SELECT owner FROM embed_chunks`); err != nil {
		t.Errorf("owner column missing: %v", err)
	}
}

// The flat store keeps a chunk's owner through a snapshot and reload.
func TestFlatStoreKeepsOwner(t *testing.T) {
	path := t.TempDir()
	open := func() Index {
		idx, err := Open(Options{Embedder: NewStubEmbedder(8), Path: path})
		if err != nil {
			t.Fatal(err)
		}
		return idx
	}
	idx := open()
	if err := idx.Add(context.Background(), Document{ID: "v1", Text: "diary", Owner: "victim"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Snapshot(); err != nil {
		t.Fatal(err)
	}
	_ = idx.Close()
	idx = open()
	t.Cleanup(func() { _ = idx.Close() })
	if got, want := docOwners(t, idx), []string{"v1:victim"}; !slices.Equal(got, want) {
		t.Errorf("after reload: %v, want %v", got, want)
	}
}

// ownerRecorder is a search backend that keeps every document indexed
// into it.
type ownerRecorder struct {
	search.Backend
	docs []search.Document
}

func (r *ownerRecorder) Index(ctx context.Context, d search.Document) error {
	r.docs = append(r.docs, d)
	return r.Backend.Index(ctx, d)
}

// Hybrid search copies each chunk's text into the keyword backend. That
// copy is the user's text too, so it carries the document's owner and a
// search store's eraser reaches it (2026-10-28 review of the erasure
// fix: the keyword copy was ownerless).
func TestKeywordCopyKeepsOwner(t *testing.T) {
	rec := &ownerRecorder{Backend: search.NewMemory()}
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Keyword: WrapSearchBackend(rec)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	if err := idx.Add(context.Background(), Document{ID: "v1", Text: "diary", Owner: "victim"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.docs) == 0 {
		t.Fatal("nothing reached the keyword backend")
	}
	for _, d := range rec.docs {
		if d.Owner != "victim" {
			t.Errorf("keyword copy %q has owner %q, want victim", d.ID, d.Owner)
		}
	}
}
