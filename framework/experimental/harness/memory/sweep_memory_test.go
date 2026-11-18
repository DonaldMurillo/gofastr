package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// Save refuses an entry with no description or a type outside the four known ones.
func TestSaveRejectsMissingDescriptionAndUnknownType(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Name: "a", Type: TypeUser}); err == nil {
		t.Error("Save accepted an entry with no description")
	}
	if err := s.Save(Entry{Name: "b", Description: "d", Type: "secret"}); err == nil {
		t.Error("Save accepted an unknown type")
	}
}

// Saving over a world-readable entry file leaves it owner-only.
func TestEntryOverwriteTightened(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "user_role.md")
	if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Name: "user_role", Description: "d", Type: TypeUser, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, p, 0o600)
}
