//go:build !windows

// Package filepermtest checks file permissions using the host's permission model.
package filepermtest

import (
	"os"
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
