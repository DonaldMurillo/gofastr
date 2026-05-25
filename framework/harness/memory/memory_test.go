package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{
		Name:        "user_role",
		Description: "User is a senior Go engineer",
		Type:        TypeUser,
		Body:        "Frame explanations in Go terms.\n",
	}); err != nil {
		t.Fatal(err)
	}
	// Verify file written.
	got, err := os.ReadFile(filepath.Join(dir, "user_role.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "name: user_role") {
		t.Errorf("file content missing frontmatter: %q", string(got))
	}
	// Verify MEMORY.md index updated.
	idx, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if !strings.Contains(string(idx), "user_role") {
		t.Errorf("MEMORY.md missing entry: %q", string(idx))
	}
	// Reload from disk.
	s2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries := s2.All()
	if len(entries) != 1 || entries[0].Name != "user_role" || entries[0].Type != TypeUser {
		t.Errorf("reloaded entries = %+v", entries)
	}
}

func TestByType(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	_ = s.Save(Entry{Name: "u", Description: "u", Type: TypeUser, Body: "a"})
	_ = s.Save(Entry{Name: "f", Description: "f", Type: TypeFeedback, Body: "b"})
	_ = s.Save(Entry{Name: "p", Description: "p", Type: TypeProject, Body: "c"})
	if len(s.ByType(TypeUser)) != 1 {
		t.Error("ByType user mismatch")
	}
	if len(s.ByType(TypeFeedback)) != 1 {
		t.Error("ByType feedback mismatch")
	}
	if len(s.ByType(TypeReference)) != 0 {
		t.Error("ByType reference should be empty")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	_ = s.Save(Entry{Name: "x", Description: "x", Type: TypeUser, Body: "a"})
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("x"); ok {
		t.Error("entry not deleted from in-memory map")
	}
	if _, err := os.Stat(filepath.Join(dir, "x.md")); !os.IsNotExist(err) {
		t.Error("file not removed")
	}
}

func TestRelevantScoring(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	_ = s.Save(Entry{Name: "no-commit-binaries", Description: "Never commit binaries", Type: TypeFeedback, Body: "Audit on every commit."})
	_ = s.Save(Entry{Name: "test-naming", Description: "Tests should be short", Type: TypeFeedback, Body: "Drop the essays."})
	_ = s.Save(Entry{Name: "harness-plan", Description: "Harness build decisions", Type: TypeProject, Body: "v0.1 ships OpenRouter + ZAI."})

	picks := s.Relevant("about to commit a binary", 2)
	if len(picks) == 0 || picks[0].Name != "no-commit-binaries" {
		var names []string
		for _, p := range picks {
			names = append(names, p.Name)
		}
		t.Errorf("relevant picks = %v, want no-commit-binaries first", names)
	}
}

func TestParseRejectsMissingFields(t *testing.T) {
	_, err := parse("---\nname: x\n---\n\nbody\n")
	if err == nil {
		t.Fatal("expected error for missing description")
	}
}
