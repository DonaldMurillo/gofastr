package render

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// A seed row that names one column twice is refused naming the same
// spelling on every apply: insertSeed ranged the row map, so the refusal
// named whichever spelling the iteration reached second.
func TestSeedFoldRefusalIsDeterministic(t *testing.T) {
	db := openSeedDB(t)
	row := map[string]any{"name": "a", "NAME": "b", "Name": "c", "nAme": "d"}
	seed := &world.Seed{Entity: "users", Rows: []map[string]any{row}}
	first := ApplySeeds(db, seedWorld(seed, ""))
	if first == nil {
		t.Fatal("a column named twice was accepted")
	}
	for range 64 {
		if err := ApplySeeds(db, seedWorld(seed, "")); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between applies: %q then %v", first, err)
		}
	}
}
