package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAGENTSMDWalkUpward(t *testing.T) {
	root := t.TempDir()
	// Layout: root/AGENTS.md, root/a/AGENTS.md, root/a/b (cwd here).
	_ = os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root rule"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "a", "AGENTS.md"), []byte("nested rule"), 0o644)
	// Put a .git in root so walkUpward stops there.
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)

	r := &Reader{
		WorkingDir: filepath.Join(root, "a", "b"),
		Sources:    []string{"AGENTS.md"},
	}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	// Deepest-first ordering.
	if !strings.Contains(sections[0].Body, "nested") {
		t.Errorf("first section = %q, expected nested rule first", sections[0].Body)
	}
	if !strings.Contains(sections[1].Body, "root") {
		t.Errorf("second section = %q, expected root rule second", sections[1].Body)
	}
	// SHA256 populated.
	if sections[0].SHA256 == "" {
		t.Errorf("missing sha256")
	}
}

func TestReadFallbackFiles(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("claude content"), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".cursorrules"), []byte("cursor content"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "GEMINI.md"), []byte("gemini content"), 0o644)
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755) // stop walkUpward here for any AGENTS hits

	r := &Reader{
		WorkingDir: root,
		Sources:    []string{"CLAUDE.md", ".cursorrules", "GEMINI.md"},
	}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 3 {
		t.Fatalf("got %d, want 3", len(sections))
	}
	wants := []string{"claude-md", "cursorrules", "gemini-md"}
	for i, w := range wants {
		if sections[i].Source != w {
			t.Errorf("section[%d].Source = %q, want %q", i, sections[i].Source, w)
		}
	}
}

func TestReadMissingFilesSilent(t *testing.T) {
	r := &Reader{
		WorkingDir: t.TempDir(),
		Sources:    []string{"AGENTS.md", "CLAUDE.md"},
	}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 0 {
		t.Errorf("got %d sections, want 0", len(sections))
	}
}

func TestReadRejectsUnknownSource(t *testing.T) {
	r := &Reader{
		WorkingDir: t.TempDir(),
		Sources:    []string{"mystery.txt"},
	}
	_, err := r.Read()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("err = %v", err)
	}
}

func TestNormalizeNewlines(t *testing.T) {
	got := normalizeNewlines("a\r\nb\rc\n")
	if got != "a\nb\nc\n" {
		t.Errorf("normalize = %q", got)
	}
}
