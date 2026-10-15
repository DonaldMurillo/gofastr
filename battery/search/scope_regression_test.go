package search

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryOwnsDocumentFields(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	f := map[string]any{"tenant": "a"}
	m.Index(ctx, Document{ID: "1", Fields: f})
	f["tenant"] = "b"
	r, _ := m.Search(ctx, Query{FieldEquals: map[string]string{"tenant": "a"}})
	if len(r) != 1 {
		t.Fatal("caller changed indexed tenant")
	}
	r[0].Document.Fields["tenant"] = "c"
	r, _ = m.Search(ctx, Query{FieldEquals: map[string]string{"tenant": "a"}})
	if len(r) != 1 {
		t.Fatal("result changed indexed tenant")
	}
}
func TestMemoryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewMemory()
	if err := m.Index(ctx, Document{ID: "1"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Index: %v", err)
	}
	if err := m.Delete(ctx, "1"); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := m.Search(ctx, Query{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Search: %v", err)
	}
}
