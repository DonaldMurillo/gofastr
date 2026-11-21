package coverageroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindUpConfinesMarker(t *testing.T) {
	parent := t.TempDir()
	start := filepath.Join(parent, "child")
	outside := t.TempDir()
	for _, dir := range []string{start, filepath.Join(parent, "markers"), outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{filepath.Join(parent, "markers"), outside} {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(start, "markers")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if got, ok := FindUp(start, filepath.Join("markers", "go.mod")); !ok || got != parent {
		t.Fatalf("FindUp = %q, %t, want %q, true", got, ok, parent)
	}
}
