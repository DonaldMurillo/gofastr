package search

import (
	"context"
	"testing"
)

func TestPostgresWeightedNonStringRefused(t *testing.T) {
	idx := newSearchIndex(t, map[string]byte{"title": 'B'})
	err := idx.Index(context.Background(), Document{ID: "1", Text: "body", Fields: map[string]any{"title": 42}})
	if err == nil {
		t.Fatal("Index accepted a non-string value for a weighted field")
	}
}

func TestMemoryCloseStopsErasure(t *testing.T) {
	m := NewMemory()
	_ = m.Index(context.Background(), Document{ID: "1", Text: "x", Owner: "u1"})
	_ = m.Index(context.Background(), Document{ID: "2", Text: "y"})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if n, _ := m.eraseOwner(context.Background(), "", false); n != 0 {
		t.Fatalf("an empty owner erased %d documents", n)
	}
	if n, _ := m.eraseOwner(context.Background(), "u1", true); n != 1 {
		t.Fatalf("a dry run counted %d documents, want 1", n)
	}
	if got, _ := m.Search(context.Background(), Query{}); len(got) != 2 {
		t.Fatalf("Close dropped documents: %d left", len(got))
	}
}
