package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesktopTypesAgainstExample runs `desktop types` against the real
// example app: the built binary must print its manifest (plugin
// capability included) and the CLI must turn it into a .d.ts. Skipped
// in -short; it compiles the example.
func TestDesktopTypesAgainstExample(t *testing.T) {
	if testing.Short() {
		t.Skip("desktop types e2e: compiles examples/desktop-notes")
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	out := filepath.Join(t.TempDir(), "desktop-notes.d.ts")

	var code int
	captured := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runDesktopTypes([]string{
				"--pkg", "./examples/desktop-notes",
				"--out", out,
			})
		})
	})
	_ = captured
	if code != -1 {
		t.Fatalf("desktop types exited %d", code)
	}

	dts, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}
	for _, want := range []string{"systeminfo", "cpuCount", "clipboard", "writeText", "window"} {
		if !strings.Contains(string(dts), want) {
			t.Errorf(".d.ts missing %q", want)
		}
	}
	if !strings.Contains(string(dts), "declare global") {
		t.Error(".d.ts is not a global declaration")
	}
}
