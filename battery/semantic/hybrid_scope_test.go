package semantic

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// pickEmbedder embeds a text by its first word: "decoy" and the query
// point one way, everything else the other. Name matches newPgStore's
// model.
type pickEmbedder struct{}

func (pickEmbedder) Dim() int     { return 2 }
func (pickEmbedder) Name() string { return "test" }
func (pickEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if strings.HasPrefix(t, "decoy") || t == "needle" {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

// The keyword leg fetched width hits, then dropped those outside the
// filter. Fifty private documents matching "needle" harder than the
// public target filled the window, and 49 public decoys nearer the
// query vector took the vector leg: the target went missing. In-scope
// keyword recall must not depend on how many out-of-scope documents
// match.
func TestHybridKeywordScopesBeforeCut(t *testing.T) {
	ctx := context.Background()
	stores := map[string]func(t *testing.T) Store{
		"flat":     func(*testing.T) Store { return nil },
		"pgvector": func(t *testing.T) Store { return newPgStore(t, 2) },
	}
	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			store := mk(t)
			var kw KeywordBackend = NewMemoryKeyword()
			if pg, ok := store.(*PgVectorStore); ok {
				kw = pg.Keyword()
			}
			idx, err := Open(Options{Embedder: pickEmbedder{}, Store: store, Keyword: kw})
			if err != nil {
				t.Fatal(err)
			}
			defer idx.Close()
			var docs []Document
			for i := range 49 {
				docs = append(docs, Document{ID: fmt.Sprintf("decoy-%02d", i), Source: "public", Text: "decoy"})
			}
			docs = append(docs, Document{ID: "target", Source: "public", Text: "needle filler"})
			for i := range 50 {
				docs = append(docs, Document{ID: fmt.Sprintf("private-%02d", i), Source: "private", Text: "needle needle"})
			}
			if err := idx.Add(ctx, docs...); err != nil {
				t.Fatal(err)
			}
			hits, err := idx.Query(ctx, Query{Text: "needle", K: 1, Hybrid: true, Filter: Filter{Source: "public"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 1 || hits[0].Chunk.DocID != "target" {
				t.Fatalf("top hit = %+v, want target: out-of-scope keyword matches crowded it out", hits)
			}
		})
	}
}
