//go:build windows

package storage_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func requirePOSIXPermissions(t *testing.T) {
	t.Helper()
	t.Skip("POSIX permission bits are not portable on Windows")
}

func assertProtectedOwnerACL(t *testing.T, path string) {
	filepermtest.AssertOwnerOnly(t, path, 0o600)
}

func TestLocalStorageWindowsACLsAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := newTestLocalStorage(t, dir).Save(context.Background(), "tenant-a/private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a"))
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a", "private"))
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a", "private", "report.txt"))
}
