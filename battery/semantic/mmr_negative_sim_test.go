package semantic

import "testing"

// The diversity penalty is the highest similarity to anything already
// picked, and that can be negative: a candidate pointing away from every
// pick is more diverse than an orthogonal one. Starting the maximum at 0
// would floor B's −1 to 0 and tie it with C, and C, listed first, would
// win. With lambda 1 (diversity only) B ranks before C.
func TestMMRNegativeSimilarityRanks(t *testing.T) {
	hit := func(id string, x, y float32) Hit {
		return Hit{Chunk: Chunk{ID: id, DocID: id, Vec: []float32{x, y}}}
	}
	hits := []Hit{hit("A", 0.6, 0.8), hit("C", 0.8, -0.6), hit("B", -0.6, -0.8)}
	out := mmr([]float32{1, 0}, hits, 1, 3)
	var got []string
	for _, h := range out {
		got = append(got, h.Chunk.ID)
	}
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("mmr order = %v, want [A B C]", got)
	}
}
