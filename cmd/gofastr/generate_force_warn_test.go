package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --force skips the one-shot conflict check by design. What it must not do
// is discard hand edits in silence: that is how examples/meridian drifted
// into an app the generator could no longer produce without anyone
// noticing (#131). The warning names every file whose content the
// generator is not about to reproduce.
func TestForceNamesHandEditedFiles(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })

	edited := filepath.Join(dir, "app.go")
	orig, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("first generate did not emit app.go: %v", err)
	}
	if err := os.WriteFile(edited, append(orig, []byte("\n// hand-written surface\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp, "--force"}) })

	if !strings.Contains(out, "hand edits") {
		t.Errorf("--force did not warn about discarded hand edits:\n%s", out)
	}
	if !strings.Contains(out, "app.go") {
		t.Errorf("warning did not name the edited file:\n%s", out)
	}
	// Files the generator reproduces byte-for-byte are not hand-edited and
	// must stay out of the list — a warning that cries wolf gets ignored.
	if strings.Contains(out, "stubs.go") {
		t.Errorf("warning named an unmodified file:\n%s", out)
	}
}

// A clean regeneration warns about nothing.
func TestForceQuietWhenNothingEdited(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML()), 0o644); err != nil {
		t.Fatal(err)
	}

	covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp}) })
	out := covT_capStdout(t, func() { runGenerate([]string{"--from=" + bp, "--force"}) })

	if strings.Contains(out, "hand edits") {
		t.Errorf("--force warned on an unmodified tree:\n%s", out)
	}
}
