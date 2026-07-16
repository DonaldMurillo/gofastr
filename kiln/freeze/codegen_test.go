package freeze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestLookForGofastrReports(t *testing.T) {
	err := LookForGofastr()
	if err != nil && !strings.Contains(err.Error(), "gofastr") {
		t.Fatalf("expected error to mention gofastr binary, got %v", err)
	}
}

func TestFreezeAndGenerateAlwaysWritesBlueprintBeforeCommand(t *testing.T) {
	w := world.New()
	w.App.Name = "demo"
	w.Entities["posts"] = &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	dir := t.TempDir()

	// An empty PATH makes the command phase deterministic while still
	// proving the new graduation artifact is written first.
	t.Setenv("PATH", "")
	err := FreezeAndGenerate(w, dir, "")
	if err == nil || !strings.Contains(err.Error(), "gofastr") {
		t.Fatalf("want missing gofastr error, got %v", err)
	}
	for _, want := range []string{"gofastr.yml", "world.json"} {
		if _, statErr := os.Stat(filepath.Join(dir, want)); statErr != nil {
			t.Errorf("missing %s after command lookup failure: %v", want, statErr)
		}
	}
}
