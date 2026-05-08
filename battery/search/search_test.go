package search

import (
	"context"
	"testing"
)

func TestMemorySearchIndexesAndSearches(t *testing.T) {
	ctx := context.Background()
	index := NewMemory()
	docs := []Document{
		{ID: "1", Type: "posts", Text: "GoFastr framework release", Fields: map[string]any{"status": "published"}},
		{ID: "2", Type: "posts", Text: "Draft notes", Fields: map[string]any{"title": "Private"}},
		{ID: "3", Type: "users", Text: "GoFastr maintainer"},
	}
	for _, doc := range docs {
		if err := index.Index(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}

	results, err := index.Search(ctx, Query{Text: "gofastr", Type: "posts", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Document.ID != "1" {
		t.Fatalf("results = %#v", results)
	}
}

func TestMemorySearchRequiresAllTerms(t *testing.T) {
	ctx := context.Background()
	index := NewMemory()
	_ = index.Index(ctx, Document{ID: "1", Text: "alpha beta gamma"})
	_ = index.Index(ctx, Document{ID: "2", Text: "alpha"})

	results, err := index.Search(ctx, Query{Text: "alpha beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Document.ID != "1" {
		t.Fatalf("results = %#v", results)
	}
}

func TestMemorySearchDelete(t *testing.T) {
	ctx := context.Background()
	index := NewMemory()
	_ = index.Index(ctx, Document{ID: "1", Text: "hello"})
	if err := index.Delete(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	results, err := index.Search(ctx, Query{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
}
