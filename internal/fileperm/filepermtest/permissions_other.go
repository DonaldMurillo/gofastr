//go:build !windows

// Package filepermtest checks file permissions using the host's permission model.
package filepermtest

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// AssertOwnerOnly requires the given Unix mode, or an owner-only Windows DACL.
func AssertOwnerOnly(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Fatalf("%s mode = %04o, want %04o", path, got, mode)
	}
}

// AssertOwnerAccess permits inheritance on Windows and checks the Unix mode here.
func AssertOwnerAccess(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	AssertOwnerOnly(t, path, mode)
}

// MakeUnreadable removes read access for a test and restores permissions at cleanup.
func MakeUnreadable(t testing.TB, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	})
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
}

// MakeUnwritable blocks writes and returns a function that restores access.
func MakeUnwritable(t testing.TB, path string) func() {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	}
	t.Cleanup(restore)
	if err := os.Chmod(path, info.Mode().Perm()&^0o222); err != nil {
		t.Fatal(err)
	}
	return restore
}

// BlockWrite makes path unwritable by the test process, including one
// running as root, and returns a function that lifts the block. Mode
// bits alone do not stop root, so on macOS the user-immutable flag is
// set (chflags uchg refuses root's open-for-write with EPERM); on other
// POSIX systems the mode bits are the best available, and a root there
// would see the test's own "write unexpectedly succeeded" fatal rather
// than a false pass. A regular file is required: the flag and the bits
// both sit on one.
func BlockWrite(t testing.TB, path string) func() {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		chflags := func(args ...string) {
			if out, cerr := exec.Command("chflags", args...).CombinedOutput(); cerr != nil {
				t.Fatalf("chflags %v %s: %v %s", args, path, cerr, out)
			}
		}
		chflags("uchg", path)
		t.Cleanup(func() { chflags("nouchg", path) })
		return func() { chflags("nouchg", path) }
	}
	restore := func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	}
	t.Cleanup(restore)
	if err := os.Chmod(path, info.Mode().Perm()&^0o222); err != nil {
		t.Fatal(err)
	}
	return restore
}
