//go:build windows

package upload_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"golang.org/x/sys/windows"
)

func requirePOSIXFileModes(t *testing.T) {
	t.Helper()
	t.Skip("POSIX permission bits are not portable on Windows")
}

func TestLocalStorageWindowsACLsAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := upload.NewLocalStorage(dir).Save(context.Background(), "tenant-a/nested/private.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dir, "tenant-a"),
		filepath.Join(dir, "tenant-a", "nested"),
		filepath.Join(dir, "tenant-a", "nested", "private.txt"),
	} {
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
		if err != nil || acl == nil || acl.AceCount == 0 {
			t.Fatalf("%s DACL has unexpected ACEs: acl=%#v err=%v SDDL=%q", path, acl, err, sd.String())
		}
		owner, _, err := sd.Owner()
		if err != nil {
			t.Fatal(err)
		}
		for _, ace := range strings.Split(sd.String(), "(")[1:] {
			if !strings.HasSuffix(strings.TrimSuffix(ace, ")"), ";;;"+owner.String()) {
				t.Fatalf("%s DACL contains non-owner ACE: %q owner=%q", path, ace, owner.String())
			}
		}
	}
}
