//go:build !windows

package agentscmd

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A new onboarding file is created the way os.WriteFile creates one: the
// process umask applies to its mode. The first os.Root writer chmodded
// every new file to exactly 0644, so a user with umask 027 got
// world-readable files they never asked for.
func TestProjectFileNewFileRespectsUmask(t *testing.T) {
	old := syscall.Umask(0o027)
	t.Cleanup(func() { syscall.Umask(old) })
	dir := t.TempDir()
	if err := WriteProjectFile(dir, "AGENTS.md", []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("new file mode = %#o, want 0640 (0644 under umask 027)", got)
	}
	// An existing file keeps its own mode, umask or not.
	if err := os.Chmod(filepath.Join(dir, "AGENTS.md"), 0o604); err != nil {
		t.Fatal(err)
	}
	if err := WriteProjectFile(dir, "AGENTS.md", []byte("y\n")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "AGENTS.md")); fi.Mode().Perm() != 0o604 {
		t.Errorf("existing file mode = %#o, want its own 0604 kept", fi.Mode().Perm())
	}
}
