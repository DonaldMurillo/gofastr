//go:build windows

package storage_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"golang.org/x/sys/windows"
)

func requirePOSIXPermissions(t *testing.T) {
	t.Helper()
	t.Skip("POSIX permission bits are not portable on Windows")
}

func assertProtectedOwnerACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("%s DACL is inheritable; got control %#x", path, control)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil {
		t.Fatalf("%s DACL is missing", path)
	}
	if acl.AceCount == 0 {
		t.Fatalf("%s DACL has no ACEs", path)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	ownerACE := ";;;" + owner.String()
	for _, ace := range strings.Split(sd.String(), "(")[1:] {
		if !strings.HasSuffix(strings.TrimSuffix(ace, ")"), ownerACE) {
			t.Fatalf("%s DACL contains non-owner ACE: %q owner=%q", path, ace, owner.String())
		}
	}
}

func TestLocalStorageWindowsACLsAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := storage.NewLocalStorage(dir).Save(context.Background(), "tenant-a/private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a"))
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a", "private"))
	assertProtectedOwnerACL(t, filepath.Join(dir, "tenant-a", "private", "report.txt"))
}
