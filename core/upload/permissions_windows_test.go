//go:build windows

package upload_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func requirePOSIXFileModes(t *testing.T) {
	t.Helper()
	t.Skip("POSIX permission bits are not portable on Windows")
}

func TestLocalStorageWindowsACLsAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := newTestLocalStorage(t, dir).Save(context.Background(), "tenant-a/nested/private.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dir, "tenant-a"),
		filepath.Join(dir, "tenant-a", "nested"),
		filepath.Join(dir, "tenant-a", "nested", "private.txt"),
	} {
		filepermtest.AssertOwnerOnly(t, path, 0o600)
	}
}
