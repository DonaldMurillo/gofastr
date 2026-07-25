package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// Property: Entry.Name becomes a filename under the store root, so it
// must never be able to walk out of it. Save builds
// <root>/<name>.md — an unvalidated name escapes the root and writes
// an attacker-chosen file with the process's own privileges.
func TestSaveRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	// The distinct escape shapes, not 60 variants of the same one.
	for _, name := range []string{
		"../escaped",
		"../../../etc/passwd",
		"sub/nested",
		`..\windows`,
		"with\x00nul",
	} {
		err := s.Save(Entry{Name: name, Description: "d", Type: TypeUser, Body: "b"})
		if err == nil {
			t.Errorf("Save(%q) was accepted; expected rejection", name)
		}
	}

	// Nothing may exist outside the root.
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escaped.md")); err == nil {
		t.Fatal("an entry escaped the memory root")
	}
}

// The happy path must still work — slug names are the convention.
func TestSaveAcceptsSlugName(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{
		Name: "user_prefers-tabs", Description: "d", Type: TypeUser, Body: "b",
	}); err != nil {
		t.Fatalf("legitimate slug name rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "user_prefers-tabs.md")); err != nil {
		t.Fatalf("entry not written to the root: %v", err)
	}
}
