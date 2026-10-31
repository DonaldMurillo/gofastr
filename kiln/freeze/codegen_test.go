package freeze

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestLookForGofastrReports(t *testing.T) {
	err := LookForGofastr()
	if err != nil && !strings.Contains(err.Error(), "gofastr") {
		t.Fatalf("expected error to mention gofastr binary, got %v", err)
	}
}

func TestFreezeAndGenerateAlwaysWritesAppSpecBeforeCommand(t *testing.T) {
	w := world.New()
	w.App.Name = "demo"
	w.Entities["posts"] = &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	dir := t.TempDir()

	// An empty PATH makes the command phase deterministic while still
	// proving the new graduation artifact is written first.
	t.Setenv("PATH", "")
	err := FreezeAndGenerate(context.Background(), w, dir, "")
	if err == nil || !strings.Contains(err.Error(), "gofastr") {
		t.Fatalf("want missing gofastr error, got %v", err)
	}
	for _, want := range []string{"gofastr.yml", "world.json"} {
		if _, statErr := os.Stat(filepath.Join(dir, want)); statErr != nil {
			t.Errorf("missing %s after command lookup failure: %v", want, statErr)
		}
	}
}

// A generator that exits while a child it started still holds the
// output pipe wedged CombinedOutput until that child exited too: no
// context and no WaitDelay bounded it (2026-10-27 K19).
func TestFreezeGenerateBoundsAHeldPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake generator is a shell script")
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\nsleep 30 &\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gofastr"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	w := world.New()
	w.App.Name = "demo"
	start := time.Now()
	_ = FreezeAndGenerate(context.Background(), w, t.TempDir(), "")
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("FreezeAndGenerate took %s behind a held pipe, want it bounded by WaitDelay", d)
	}
}
