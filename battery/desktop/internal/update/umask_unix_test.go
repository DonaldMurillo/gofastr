//go:build unix

package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// withUmask runs the test body under mask. Under the usual 022 a create
// call already yields 0755, so a mode bug only shows under a tighter one.
// The umask is process-wide: callers must not run in parallel.
func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestCopyTreeModeUnderTightUmask(t *testing.T) {
	withUmask(t, 0o077)
	src := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(src, []byte("bin"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("copied mode %o, want 755", info.Mode().Perm())
	}
}

func TestExtractModeUnderTightUmask(t *testing.T) {
	withUmask(t, 0o077)
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":    {nil, fs.ModeDir | 0o755},
		"Notes.app/bin": {[]byte("bin"), 0o755},
	})
	app, err := ExtractZip(data, t.TempDir(), "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(app, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("extracted mode %o, want 755", info.Mode().Perm())
	}
}

// A zip built on Windows or by a careless tool records 0777. Group and
// other write on a file in the installed bundle lets any local user
// swap the binary the next launch runs.
func TestExtractDropsGroupOtherWrite(t *testing.T) {
	withUmask(t, 0)
	data := buildZip(t, map[string]struct {
		content []byte
		mode    fs.FileMode
	}{
		"Notes.app/":    {nil, fs.ModeDir | 0o777},
		"Notes.app/bin": {[]byte("bin"), 0o777},
	})
	app, err := ExtractZip(data, t.TempDir(), "Notes.app")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(app, "bin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("extracted file mode %o keeps group/other write", info.Mode().Perm())
	}
}

// MoveTree's copy path runs after its absence check; a destination that
// appears in between must stop the copy, not take a merge of both trees.
func TestCopyTreeRefusesExistingDestination(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "bin"), []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "bin"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dst); err == nil {
		t.Fatal("copyTree wrote into an existing destination")
	}
	got, _ := os.ReadFile(filepath.Join(dst, "bin"))
	if string(got) != "old" {
		t.Fatalf("existing destination overwritten: %q", got)
	}
}
