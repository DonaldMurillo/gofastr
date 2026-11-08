package rootwrite_test

import (
	"os"
	"path/filepath"
	"testing"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/rootwrite/testdata/src/k"
)

// The quiet postures in fixture k must confine for real: a link inside
// the root that points outside is refused, and the outside file keeps
// its bytes (#665).
func TestQuietPosturesRefuseOutsideLink(t *testing.T) {
	for name, write := range map[string]func(string, string, []byte) error{
		"Contained": fixture.Contained,
		"ViaRoot":   fixture.ViaRoot,
	} {
		t.Run(name, func(t *testing.T) {
			base, outside := t.TempDir(), t.TempDir()
			victim := filepath.Join(outside, "file")
			if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
				t.Fatal(err)
			}
			if err := write(base, "link/file", []byte("pwned")); err == nil {
				t.Fatal("wrote through a link that leaves the root")
			}
			if got, _ := os.ReadFile(victim); string(got) != "keep" {
				t.Fatalf("outside file = %q", got)
			}
		})
	}
}

func TestContainedWritesInsideRoot(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "file")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Contained(base, "file", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "ok" {
		t.Fatalf("file = %q", got)
	}
}
