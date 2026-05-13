package freeze

import (
	"os/exec"
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

// TestFreezeAndGenerate_RealBinary runs the codegen end-to-end via the
// installed gofastr binary. Skips if gofastr is not on PATH so this test
// is a no-op on machines without the toolchain.
func TestFreezeAndGenerate_RealBinary(t *testing.T) {
	if _, err := exec.LookPath("gofastr"); err != nil {
		t.Skip("gofastr binary not on PATH; run `go install ./cmd/gofastr` to enable this test")
	}

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
	if err := FreezeAndGenerate(w, dir, "gen"); err != nil {
		t.Fatalf("FreezeAndGenerate: %v", err)
	}
	for _, want := range []string{
		filepath.Join(dir, "entities", "posts.json"),
		filepath.Join(dir, "world.json"),
		filepath.Join(dir, "gen", "register.go"),
		filepath.Join(dir, "gen", "models.go"),
		filepath.Join(dir, "gen", "columns.go"),
		filepath.Join(dir, "gen", "repo.go"),
	} {
		if _, err := exec.Command("test", "-f", want).Output(); err != nil {
			// fallback: stat via os
			if !fileExists(want) {
				t.Errorf("missing expected file: %s", want)
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := exec.Command("test", "-f", path).Output()
	if err == nil {
		return true
	}
	// stat fallback
	out, _ := exec.Command("ls", path).CombinedOutput()
	return len(out) > 0 && !strings.Contains(string(out), "No such")
}
