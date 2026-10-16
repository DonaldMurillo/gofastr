package search

import (
	"context"
	"fmt"
	"testing"
)

// Search copies the Fields of the documents it returns, not of every
// match: a broad query with Limit 1 must not copy each matching map.
func TestSearchCopiesOnlyReturnedFields(t *testing.T) {
	const n = 300
	m := NewMemory()
	fields := map[string]any{}
	for i := range 64 {
		fields[fmt.Sprintf("f%02d", i)] = i
	}
	for i := range n {
		doc := Document{ID: fmt.Sprintf("d%03d", i), Type: "t", Text: "needle", Fields: fields}
		if err := m.Index(context.Background(), doc); err != nil {
			t.Fatal(err)
		}
	}
	measure := func(limit int) float64 {
		return testing.AllocsPerRun(3, func() {
			if _, err := m.Search(context.Background(), Query{Text: "needle", Limit: limit}); err != nil {
				t.Fatal(err)
			}
		})
	}
	one, all := measure(1), measure(n)
	// Copying n 64-entry maps costs several allocations each; returning
	// one hit must not pay for them.
	if all-one < n {
		t.Fatalf("Limit 1 made %.0f allocations, Limit %d made %.0f: every match's Fields is copied", one, n, all)
	}
}
