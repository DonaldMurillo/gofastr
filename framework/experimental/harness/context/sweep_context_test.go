package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStopsAtRepoRoot(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "AGENTS.md"), []byte("outside repo"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("repo rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	sections, err := (&Reader{WorkingDir: repo, Sources: []string{"AGENTS.md"}}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || sections[0].Body != "repo rule" {
		t.Fatalf("sections = %+v, want only the repo's AGENTS.md", sections)
	}
}

func TestReadSkipsRefusedFileKeepsRest(t *testing.T) {
	root := repoDir(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root rule"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", maxContextFileBytes+1)
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	sections, err := (&Reader{WorkingDir: sub, Sources: []string{"AGENTS.md"}}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || sections[0].Body != "root rule" {
		t.Fatalf("sections = %+v, want the root AGENTS.md only", sections)
	}
}
