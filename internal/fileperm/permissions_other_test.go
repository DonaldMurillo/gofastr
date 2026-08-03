//go:build !windows

package fileperm

import (
	"os"
	"path/filepath"
	"testing"
)

// On Unix, Restrict and RestrictDirectoryTree are no-ops: they ignore their
// arguments and return nil. Restriction on Unix comes from the 0o600/0o700
// modes the call sites pass to open/mkdir/chmod, not from these functions.
//
// These tests pin that contract. A future change that made Restrict silently
// tighten *or* loosen a mode on Unix would break callers that rely on it never
// touching the mode, so both directions are covered.

// restrictEntry creates a path under dir that is either a file or a directory
// and then forces its permission bits to mode with an explicit Chmod, bypassing
// umask so the assertion is exact rather than "at least this strict".
func restrictEntry(t *testing.T, dir, name string, isDir bool, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if isDir {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	} else {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

func TestRestrictPreservesMode(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name  string
		isDir bool
		mode  os.FileMode
	}{
		{"ownerOnlyFile", false, 0o600},
		{"groupReadableFile", false, 0o644}, // must not be loosened *or* tightened
		{"ownerOnlyDir", true, 0o700},
		{"groupReadableDir", true, 0o755},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := restrictEntry(t, dir, tc.name, tc.isDir, tc.mode)
			if err := Restrict(path, tc.isDir); err != nil {
				t.Fatalf("Restrict(%q, %v) = %v, want nil", path, tc.isDir, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != tc.mode {
				t.Fatalf("mode = %o, want %o (Restrict must not change mode on Unix)", got, tc.mode)
			}
		})
	}
}

// Restrict ignores its path argument entirely on Unix, so a missing path is
// neither stat'd nor rejected — it returns nil. (The Windows implementation
// would fail here when reading the ACL; that divergence is expected for a
// no-op.)
func TestRestrictMissingPathReturnsNil(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := Restrict(missing, false); err != nil {
		t.Fatalf("Restrict on missing path = %v, want nil", err)
	}
}

// RestrictDirectoryTree is a no-op on Unix: it returns nil and leaves the mode
// of the target directory and every intermediate directory between root and the
// target untouched.
func TestRestrictDirectoryTreePreservesModes(t *testing.T) {
	base := t.TempDir()
	leaf := filepath.Join(base, "a", "b", "c")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pin distinct, non-default modes on each component so a future change
	// that walks and chmods them would be caught.
	components := []string{
		filepath.Join(base, "a"),
		filepath.Join(base, "a", "b"),
		leaf,
	}
	want := make(map[string]os.FileMode, len(components))
	for _, p := range components {
		if err := os.Chmod(p, 0o751); err != nil {
			t.Fatalf("chmod %s: %v", p, err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		want[p] = info.Mode().Perm()
	}

	if err := RestrictDirectoryTree(leaf, base); err != nil {
		t.Fatalf("RestrictDirectoryTree = %v, want nil", err)
	}
	for p, w := range want {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != w {
			t.Fatalf("mode of %s = %o, want unchanged %o", p, got, w)
		}
	}
}

// When the target is not under root, the Unix implementation still returns nil:
// it is a pure no-op and never checks containment. This is a deliberate
// divergence from the Windows implementation, which returns an error in that
// case. Containment of the path under root is the caller's responsibility on
// Unix (and is enforced by the 0o600/0o700 modes applied at the call sites).
func TestRestrictDirectoryTreeOutsideBaseReturnsNil(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir() // a sibling temp dir, not under base
	if err := RestrictDirectoryTree(outside, base); err != nil {
		t.Fatalf("RestrictDirectoryTree(outside, base) = %v, want nil", err)
	}
}
