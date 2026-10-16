package semantic

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// Candidates copies the hits it returns, not every chunk the filter
// passes: a 100k-chunk corpus at 768 dims would otherwise copy ~300 MB
// per query to return ten hits.
func TestCandidatesCopiesOnlyReturnedHits(t *testing.T) {
	const n = 500
	s := NewFlatStore(4, "m")
	chunks := make([]Chunk, n)
	for i := range chunks {
		chunks[i] = Chunk{
			ID: fmt.Sprintf("c%d", i), DocID: fmt.Sprintf("d%d", i),
			Metadata: map[string]any{"k": i},
			Vec:      []float32{1, float32(i), 0, 0},
		}
	}
	if err := s.Add(context.Background(), chunks); err != nil {
		t.Fatal(err)
	}
	q := []float32{1, 0, 0, 0}
	allocs := testing.AllocsPerRun(5, func() {
		if _, err := s.Candidates(context.Background(), q, Filter{}, 1); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > n/10 {
		t.Fatalf("Candidates(top=1) over %d chunks made %.0f allocations; it copies every candidate", n, allocs)
	}
}

type statsCountingStore struct {
	*FlatStore
	stats atomic.Int64
}

func (s *statsCountingStore) Stats() Stats {
	s.stats.Add(1)
	return s.FlatStore.Stats()
}

// Add checks vector dimensions against a cached store dimension: on
// pgvector, Stats is a full-table COUNT under its own timeout, which no
// write should pay for or wait on.
func TestAddDoesNotCallStatsPerWrite(t *testing.T) {
	store := &statsCountingStore{FlatStore: NewFlatStore(4, "stub-4")}
	idx, err := Open(Options{Embedder: NewStubEmbedder(4), Store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	before := store.stats.Load()
	for i := range 5 {
		if err := idx.Add(context.Background(), Document{ID: fmt.Sprintf("d%d", i), Text: "content"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := store.stats.Load() - before; n != 0 {
		t.Fatalf("5 Adds made %d Stats calls", n)
	}
}
