package semantic

import (
	"context"
	"math"
	"testing"
)

// A batch with one chunk that cannot be stored leaves none stored, as
// FlatStore.Add does.
func TestPgVectorAddIsAllOrNothing(t *testing.T) {
	store := newPgStore(t, 3)
	err := store.Add(context.Background(), []Chunk{
		{ID: "ok", DocID: "d", Text: "a", Vec: vec3(1, 0, 0)},
		{ID: "bad", DocID: "d", Text: "b", Vec: vec3(0, 1, 0), Metadata: map[string]any{"x": math.NaN()}},
	})
	if err == nil {
		t.Fatal("NaN metadata accepted")
	}
	if _, ok := store.ChunkByID("ok"); ok {
		t.Fatal("rejected batch left its first chunk stored")
	}
}
