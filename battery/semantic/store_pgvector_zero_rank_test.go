package semantic

import (
	"context"
	"testing"
)

// The zero-vector fix mapped pgvector's NaN distance to score 0 after the
// SQL had already ordered by the raw distance, where NaN sorts last: a
// zero-vector chunk ranked below a negative match, and a LIMIT cut it
// (2026-10-27 B7). The ORDER BY ranks NaN as score 0 too.
func TestPgVectorZeroVectorRanksAtScoreZero(t *testing.T) {
	s := newPgStore(t, 3)
	ctx := context.Background()
	if err := s.Add(ctx, []Chunk{
		{ID: "neg", DocID: "d1", Text: "opposite", Vec: vec3(-1, 0, 0)},
		{ID: "zero", DocID: "d2", Text: "empty", Vec: vec3(0, 0, 0)},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hits, err := s.Candidates(ctx, vec3(1, 0, 0), Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chunk.ID != "zero" {
		t.Fatalf("top hit = %+v, want the zero vector (score 0) over the negative match (score -1)", hits)
	}
}
