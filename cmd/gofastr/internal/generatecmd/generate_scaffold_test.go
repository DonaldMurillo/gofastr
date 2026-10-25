package generatecmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ---- Test 8: `generate entity <name>` no longer prints the removal tombstone ----

func TestScaffoldEntityNoLongerTombstone(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteAddGoMod(t, dir, "example.com/scaffold")
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() {
			RunGenerate([]string{"entity", "posts"})
		})
	})
	if strings.Contains(out, "has been removed") {
		t.Fatalf("`generate entity posts` still prints the removal tombstone:\n%s", out)
	}
	if code != -1 {
		t.Fatalf("expected no osExit for `generate entity posts`, got code %d", code)
	}
	if !clitest.FileExists(dir, "entities/posts.go") {
		t.Errorf("entities/posts.go not written:\n%s", out)
	}
}

// ---- Test 5: missing name / extra args → exit 1 with usage ----

func TestScaffoldEntityMissingNameUsage(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	// parseScaffoldArgs prints usage and returns ok=false (no osExit), so the
	// guidance text is capturable.
	var ok bool
	out := clitest.CovT_capStdout(t, func() {
		_, _, ok = appgen.ParseScaffoldArgs([]string{}, "entity")
	})
	if ok {
		t.Fatal("expected ok=false for `generate entity` with no name")
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "Usage") {
		t.Errorf("expected usage guidance for missing name:\n%s", out)
	}
	// The dispatch path must exit non-zero on the same input.
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			RunGenerate([]string{"entity"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for `generate entity` with no name, got %d", code)
	}
}

func TestScaffoldEntityExtraArgsUsage(t *testing.T) {
	var ok bool
	out := clitest.CovT_capStdout(t, func() {
		_, _, ok = appgen.ParseScaffoldArgs([]string{"a", "b"}, "entity")
	})
	if ok {
		t.Fatal("expected ok=false for `generate entity a b` (extra positional)")
	}
	if !strings.Contains(out, "exactly one name") {
		t.Errorf("expected 'exactly one name' guidance:\n%s", out)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() {
			RunGenerate([]string{"entity", "a", "b"})
		})
	})
	if code != 1 {
		t.Fatalf("want exit 1 for `generate entity a b` (extra arg), got %d", code)
	}
}
