package semantic

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/search"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite's unicode61 tokenizer indexes "H₂O" as one token, h₂o: the
// subscript two is a number (category No). A hybrid query for "H₂O"
// must find the document through the SQLite keyword backend and through
// MemoryKeyword alike.
func TestHybridFindsSubscriptTerm(t *testing.T) {
	ctx := context.Background()
	backends := map[string]func(t *testing.T) KeywordBackend{
		"memory": func(*testing.T) KeywordBackend { return NewMemoryKeyword() },
		"sqlite": func(t *testing.T) KeywordBackend {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			fts, err := search.NewSQLiteFTS(db, search.SQLiteFTSConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if err := fts.EnsureSchema(ctx); err != nil {
				t.Skipf("FTS5 unavailable: %v", err)
			}
			return WrapSearchBackend(fts)
		},
	}
	for name, mk := range backends {
		t.Run(name, func(t *testing.T) {
			kw := mk(t)
			// The vector leg cannot help: the stub embedder hashes
			// tokens, and both decoys share none with the target.
			idx, err := Open(Options{Embedder: NewStubEmbedder(64), Keyword: kw})
			if err != nil {
				t.Fatal(err)
			}
			if err := idx.Add(ctx,
				Document{ID: "water", Text: "H₂O"},
				Document{ID: "decoy1", Text: "quarterly revenue forecast"},
				Document{ID: "decoy2", Text: "garden irrigation schedule"},
			); err != nil {
				t.Fatal(err)
			}
			kwHits, err := kw.Search(ctx, "H₂O", 5)
			if err != nil {
				t.Fatal(err)
			}
			if len(kwHits) == 0 {
				t.Fatalf("keyword leg found nothing for H₂O")
			}
			hits, err := idx.Query(ctx, Query{Text: "H₂O", K: 1, Hybrid: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 || hits[0].Chunk.DocID != "water" {
				t.Fatalf("hybrid hits for H₂O = %+v, want water", hits)
			}
		})
	}
}

// The 64-term cap on the keyword leg counts terms the way the backends
// split them. Counted at ASCII digits only, "H₂O" was two terms, so a
// query whose 64th term was "H₂O" was cut to "… H₂".
func TestKeywordCapKeepsSubscriptWhole(t *testing.T) {
	words := make([]string, 0, maxKeywordQueryTerms)
	for i := range maxKeywordQueryTerms - 1 {
		words = append(words, "w"+strings.Repeat("x", i+1))
	}
	q := strings.Join(words, " ") + " H₂O"
	if got := boundedKeywordText(q); got != q {
		t.Fatalf("bounded text ends %q, want the whole of H₂O kept", got[max(0, len(got)-12):])
	}
}

// MemoryKeyword splits terms the way unicode61 does: "H₂O" is one term,
// so a document holding a bare H and O is not a match for it.
func TestMemoryKeywordSubscriptIsOneTerm(t *testing.T) {
	ctx := context.Background()
	kw := NewMemoryKeyword()
	if err := kw.Index(ctx, "letters", "o and h"); err != nil {
		t.Fatal(err)
	}
	if err := kw.Index(ctx, "water", "h₂o"); err != nil {
		t.Fatal(err)
	}
	hits, err := kw.Search(ctx, "H₂O", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ChunkID != "water" {
		t.Fatalf("hits for H₂O = %+v, want only water", hits)
	}
}
