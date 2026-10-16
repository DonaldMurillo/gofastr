package semantic

import (
	"context"
	"math"
	"testing"
)

// A batch with one chunk that cannot be stored leaves none stored, as
// FlatStore.Add does: whether the chunk fails while encoding (NaN
// metadata) or in the database after an earlier chunk's insert ran (a
// NaN vector, which pgvector refuses).
func TestPgVectorAddIsAllOrNothing(t *testing.T) {
	for name, bad := range map[string]Chunk{
		"meta":   {ID: "bad", DocID: "d", Text: "b", Vec: vec3(0, 1, 0), Metadata: map[string]any{"x": math.NaN()}},
		"vector": {ID: "bad", DocID: "d", Text: "b", Vec: vec3(0, 1, float32(math.NaN()))},
	} {
		t.Run(name, func(t *testing.T) {
			store := newPgStore(t, 3)
			err := store.Add(context.Background(), []Chunk{
				{ID: "ok", DocID: "d", Text: "a", Vec: vec3(1, 0, 0)},
				bad,
			})
			if err == nil {
				t.Fatal("unstorable chunk accepted")
			}
			if _, ok := store.ChunkByID("ok"); ok {
				t.Fatal("rejected batch left its first chunk stored")
			}
		})
	}
}
