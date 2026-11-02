package semantic

import (
	"context"
	"testing"
)

// scaledEmbedder maps each known text to a fixed direction, scaled by
// scale: an embedder that does not normalize its output.
type scaledEmbedder struct{ scale float32 }

var scaledDirections = map[string][]float32{
	"query":    {1, 0, 0},
	"alpha":    {1, 0, 0},
	"alpha-ii": {0.99, 0.14, 0},
	"gamma":    {0.7, 0, 0.71},
}

func (e scaledEmbedder) Name() string { return "scaled" }
func (e scaledEmbedder) Dim() int     { return 3 }
func (e scaledEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := append([]float32(nil), scaledDirections[t]...)
		for j := range v {
			v[j] *= e.scale
		}
		out[i] = v
	}
	return out, nil
}

// The stores normalize every vector they keep, so cosine ranking does
// not depend on an embedder's vector lengths, but MMR scored relevance
// against the query vector as the embedder returned it. A query vector
// ten units long made relevance ten times the redundancy penalty, and
// MMR picked the near-duplicate a unit-length query skips. MMR ranks
// the same whatever length the embedder gives the query.
func TestMMRIgnoresQueryVectorLength(t *testing.T) {
	ctx := context.Background()
	order := func(scale float32) []string {
		idx, err := Open(Options{Embedder: scaledEmbedder{scale: scale}})
		if err != nil {
			t.Fatal(err)
		}
		defer idx.Close()
		for _, id := range []string{"alpha", "alpha-ii", "gamma"} {
			if err := idx.Add(ctx, Document{ID: id, Text: id}); err != nil {
				t.Fatal(err)
			}
		}
		hits, err := idx.Query(ctx, Query{Text: "query", K: 2, MMRLambda: 0.6})
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, h := range hits {
			ids = append(ids, h.Chunk.DocID)
		}
		return ids
	}
	unit, long := order(1), order(10)
	if len(unit) != 2 || unit[0] != "alpha" || unit[1] != "gamma" {
		t.Fatalf("unit-length query picked %v, want [alpha gamma]", unit)
	}
	if long[0] != unit[0] || long[1] != unit[1] {
		t.Errorf("a query vector ten units long picked %v, a unit one %v", long, unit)
	}
}
