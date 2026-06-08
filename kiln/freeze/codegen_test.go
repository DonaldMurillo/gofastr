package freeze

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// TestLookForGofastr_AbsentReturnsError confirms the soft-detect helper
// returns a useful error when the binary isn't on PATH. We can't reliably
// fake "not on PATH" at process scope; instead, this test just asserts
// LookForGofastr's error wording when gofastr happens to be missing in the
// CI sandbox. When it's present, we exercise the success path instead.
func TestLookForGofastr_Reports(t *testing.T) {
	err := LookForGofastr()
	if err != nil {
		// gofastr not installed — verify the message is informative.
		if !strings.Contains(err.Error(), "gofastr") {
			t.Fatalf("expected error to mention gofastr binary, got %v", err)
		}
	}
	// When gofastr IS on PATH, err is nil — nothing to assert beyond that.
}

// TestFreezeAndGenerate_WritesSnapshotDefersCodegen verifies the snapshot is
// written and that automated codegen is deferred to the blueprint flow. The
// legacy entities/*.json → gen/ codegen path was removed; FreezeAndGenerate
// now returns ErrGenerateViaBlueprint after writing the snapshot.
func TestFreezeAndGenerate_WritesSnapshotDefersCodegen(t *testing.T) {
	w := world.New()
	w.Entities["posts"] = &world.Entity{
		Name:  "posts",
		Table: "posts",
		Fields: []world.Field{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "text"},
		},
	}
	dir := t.TempDir()
	err := FreezeAndGenerate(w, dir, "gen")
	if !errors.Is(err, ErrGenerateViaBlueprint) {
		t.Fatalf("want ErrGenerateViaBlueprint, got %v", err)
	}
	// The snapshot artifacts are still written.
	for _, want := range []string{
		filepath.Join(dir, "entities", "posts.json"),
		filepath.Join(dir, "world.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing snapshot file %s: %v", want, err)
		}
	}
}
