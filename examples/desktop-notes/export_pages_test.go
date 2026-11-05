package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportIncludesEveryOwnedNote(t *testing.T) {
	harness := newHarness(t)
	for index := 0; index < 1001; index++ {
		createNote(t, harness, fmt.Sprintf("Note %04d", index), "body")
	}
	target := filepath.Join(t.TempDir(), "notes.md")
	harness.Shell.SetSaveFile(target, nil)
	harness.ClickMenu("File", "Export all…")
	harness.WaitEvent("notes_exported")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "# Note "); count != 1001 {
		t.Fatalf("export contains %d notes, want 1001", count)
	}
	for index := 0; index < 1001; index++ {
		if title := fmt.Sprintf("# Note %04d\n", index); strings.Count(string(data), title) != 1 {
			t.Errorf("export must contain %q exactly once", title)
		}
	}
}
