package static

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// Pins the fallback containment check the 2026-09-04 red-probe round
// added beside the os.Root path: when a request is served through an
// fs.FS that is not *os.File-backed, or before a root is resolved, the
// check cannot see symlinks and must not refuse; when it can resolve the
// opened file it refuses anything outside the resolved root.
// Property: fileInRoot answers false only for an *os.File whose resolved
// path leaves the resolved root.
// Surfaces: core/static/static.go fileInRoot, resolveFSRoot.
func TestFileInRootFallbacks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("in"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "out.txt"), []byte("out"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{resolvedRoot: resolvedRoot}

	// A non-*os.File handle (embed.FS / MapFS) carries no symlinks: pass.
	mapFS := fstest.MapFS{"a.txt": {Data: []byte("a")}}
	mf, err := mapFS.Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	if !cfg.fileInRoot(mf) {
		t.Fatalf("non-os.File handle must pass the fallback check")
	}

	in, err := os.Open(filepath.Join(root, "in.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !cfg.fileInRoot(in) {
		t.Fatalf("file inside the resolved root must pass")
	}

	out, err := os.Open(filepath.Join(outside, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if cfg.fileInRoot(out) {
		t.Fatalf("SECURITY: file outside the resolved root passed the containment check")
	}

	// No resolved root (a root that could not be opened): the check has
	// nothing to compare against and must not refuse everything.
	if !(Config{}).fileInRoot(out) {
		t.Fatalf("with no resolved root the fallback must pass")
	}

	// The opened path vanished between open and check: refuse.
	gone := filepath.Join(root, "gone.txt")
	if err := os.WriteFile(gone, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := os.Open(gone)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if cfg.fileInRoot(g) {
		t.Fatalf("a handle whose path can no longer be resolved must be refused")
	}
}
