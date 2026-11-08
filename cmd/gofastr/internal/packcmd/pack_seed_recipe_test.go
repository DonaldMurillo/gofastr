package packcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// seedRecipeFixture is one entity whose seed carries a recipe: an authored
// row plus count/weights. Two recipes with the same count but different
// weights expand to the same explicit rows, so the rows alone cannot tell
// them apart — the recipe has to survive into stubs.go for pack to read
// it back (#669 R8-PR-02).
var seedRecipeEntity = framework.EntityDeclaration{
	Name: "tickets",
	Fields: []framework.FieldDeclaration{
		{Name: "title", Type: "string"},
		{Name: "status", Type: "enum", Values: []string{"open", "closed"}},
	},
}

func seedRecipeSpec() appspec.AppSpec {
	return appspec.AppSpec{
		Entities: []framework.EntityDeclaration{seedRecipeEntity},
		Seed: []appspec.AppSpecSeedEntity{{
			Entity:  "tickets",
			Count:   3,
			Weights: map[string]map[string]int{"status": {"open": 5, "closed": 1}},
			Rows:    []map[string]any{{"title": "first", "status": "open"}},
		}},
	}
}

// writeExpandedStubs expands the recipe the way generate app does and
// writes the emitted stubs.go into dir, returning its content.
func writeExpandedStubs(t *testing.T, dir string) string {
	t.Helper()
	bp := appgen.ExpandAppSpecSeed(seedRecipeSpec())
	if len(bp.Seed[0].Rows) != 4 {
		t.Fatalf("fixture does not exercise the recipe: %d rows after expansion", len(bp.Seed[0].Rows))
	}
	if err := os.WriteFile(filepath.Join(dir, "stubs.go"), []byte(appgen.RenderAppSpecStubs(bp)), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "stubs.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPack_SeedRecipeRoundTrips proves the recipe survives pack: the
// emitted seedEntity carries Count, Weights and Authored beside the rows,
// and packReadSeed keeps the authored row, regenerates the rest to check
// them, and returns the recipe. Before the fix the recipe expanded into
// plain rows before stubs.go was written, so pack returned four rows with
// no count or weights.
func TestPack_SeedRecipeRoundTrips(t *testing.T) {
	dir := t.TempDir()
	writeExpandedStubs(t, dir)

	seed, err := packReadSeed(dir, seedRecipeEntityIndex())
	if err != nil {
		t.Fatalf("packReadSeed: %v", err)
	}
	if len(seed) != 1 {
		t.Fatalf("pack read %d seed entities, want 1", len(seed))
	}
	got := seed[0]
	if got.Count != 3 {
		t.Errorf("pack lost the seed recipe: Count = %d, want 3", got.Count)
	}
	if got.Weights["status"]["open"] != 5 || got.Weights["status"]["closed"] != 1 {
		t.Errorf("pack lost the seed weights: %v", got.Weights)
	}
	if len(got.Rows) != 1 || got.Rows[0]["title"] != "first" {
		t.Errorf("pack kept the generated rows as explicit data: %+v", got.Rows)
	}
}

// TestPack_SeedRecipeHandEditKeptAsRows proves the edit survives: a
// generated row the author changed no longer matches what the recipe
// regenerates, so pack keeps every row as explicit data and drops the
// recipe instead of silently reverting the edit on the next generate.
func TestPack_SeedRecipeHandEditKeptAsRows(t *testing.T) {
	dir := t.TempDir()
	src := writeExpandedStubs(t, dir)
	// Hand-edit the first generated row's title (row index 1: the authored
	// row is first).
	edited := strings.Replace(src, `"Ticket 1"`, `"hand-edited"`, 1)
	if edited == src {
		t.Fatalf("fixture does not exercise the edit: no generated row matched")
	}
	if err := os.WriteFile(filepath.Join(dir, "stubs.go"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	seed, err := packReadSeed(dir, seedRecipeEntityIndex())
	if err != nil {
		t.Fatalf("packReadSeed: %v", err)
	}
	got := seed[0]
	if got.Count != 0 || got.Weights != nil {
		t.Errorf("pack kept the recipe over a hand-edited row: count=%d weights=%v", got.Count, got.Weights)
	}
	if len(got.Rows) != 4 {
		t.Fatalf("pack dropped rows while dropping the recipe: %d rows, want 4", len(got.Rows))
	}
	sawEdit := false
	for _, row := range got.Rows {
		if row["title"] == "hand-edited" {
			sawEdit = true
		}
	}
	if !sawEdit {
		t.Errorf("the hand-edited row did not survive pack: %+v", got.Rows)
	}
}

// seedRecipeEntityIndex is the entity map pack regenerates against.
func seedRecipeEntityIndex() map[string]framework.EntityDeclaration {
	return map[string]framework.EntityDeclaration{"tickets": seedRecipeEntity}
}
