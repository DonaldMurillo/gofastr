package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoDir makes a working directory that is its own repo root, so the
// AGENTS.md walk stops there.
func repoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A context file that is a symlink out of its directory is not read:
// AGENTS.md -> ~/.ssh/id_ed25519 would otherwise go to the provider in
// every system prompt.
func TestReadRefusesSymlinkEscape(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := repoDir(t)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.Symlink(secret, filepath.Join(dir, name)); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
	}
	r := &Reader{WorkingDir: dir, Sources: []string{"AGENTS.md", "CLAUDE.md"}}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sections {
		if strings.Contains(s.Body, "PRIVATE KEY") {
			t.Fatalf("%s read through a symlink out of the repo", s.Path)
		}
	}
}

// A symlink that stays inside the directory still reads.
func TestReadFollowsSymlinkInsideDir(t *testing.T) {
	dir := repoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "rules.md"), []byte("inside rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("rules.md", filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	r := &Reader{WorkingDir: dir, Sources: []string{"AGENTS.md"}}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || sections[0].Body != "inside rule" {
		t.Fatalf("sections = %+v", sections)
	}
}

// A context file past the size cap is skipped, not read whole into
// memory and into every prompt.
func TestReadSkipsOversizedFile(t *testing.T) {
	dir := repoDir(t)
	big := strings.Repeat("x", maxContextFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Reader{WorkingDir: dir, Sources: []string{"AGENTS.md"}}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 0 {
		t.Fatalf("oversized file read: %d bytes", len(sections[0].Body))
	}
}

// A .github directory linked out of the repo is refused too: the root
// is the working directory, not the file's own directory.
func TestReadRefusesLinkedGithubDir(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "copilot-instructions.md"), []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := repoDir(t)
	if err := os.Symlink(outside, filepath.Join(dir, ".github")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	r := &Reader{WorkingDir: dir, Sources: []string{".github/copilot-instructions.md"}}
	sections, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 0 {
		t.Fatalf("read through a linked .github: %+v", sections)
	}
}
