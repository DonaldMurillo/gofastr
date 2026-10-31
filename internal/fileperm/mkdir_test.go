//go:build !windows

package fileperm

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// An existing directory keeps its mode; only the components the call
// creates are owner-only (2026-10-27 L20, S43, B62).
func TestMkdirPrivateLeavesExistingDirs(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(shared, "a", "b")
	if err := MkdirPrivate(leaf); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(shared); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]os.FileMode{shared: 0o755, filepath.Join(shared, "a"): 0o700, leaf: 0o700} {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", dir, got, want)
		}
	}
}

func TestMkdirPrivateReportsMkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write bits")
	}
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := MkdirPrivate(filepath.Join(locked, "x")); err == nil {
		t.Fatal("MkdirPrivate under a read-only directory succeeded")
	}
}

// A component another process creates between the probe and the Mkdir
// is theirs: it keeps its mode, and the walk carries on below it.
func TestMkdirPrivateLeavesARacedDir(t *testing.T) {
	root := t.TempDir()
	raced := filepath.Join(root, "raced")
	leaf := filepath.Join(raced, "leaf")
	mkdir = func(d string, perm os.FileMode) error {
		if d == raced {
			if err := os.Mkdir(d, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(d, 0o755); err != nil {
				return err
			}
			return fs.ErrExist
		}
		return os.Mkdir(d, perm)
	}
	t.Cleanup(func() { mkdir = os.Mkdir })
	if err := MkdirPrivate(leaf); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]os.FileMode{raced: 0o755, leaf: 0o700} {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", dir, got, want)
		}
	}
}

func TestMkdirPrivateReportsChmodError(t *testing.T) {
	chmod = func(string, os.FileMode) error { return fs.ErrPermission }
	t.Cleanup(func() { chmod = os.Chmod })
	if err := MkdirPrivate(filepath.Join(t.TempDir(), "x")); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("MkdirPrivate = %v, want the chmod error", err)
	}
}

func TestMkdirPrivateRefusesAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(f); err == nil {
		t.Fatal("MkdirPrivate on a regular file succeeded")
	}
	if err := MkdirPrivate(filepath.Join(f, "sub")); err == nil {
		t.Fatal("MkdirPrivate under a regular file succeeded")
	}
}
