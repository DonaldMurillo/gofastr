//go:build windows

package freeze_test

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	"golang.org/x/sys/windows"
)

func TestFreezeWorldSnapshotWindowsACLIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := freeze.Freeze(world.New(), dir); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(filepath.Join(dir, "world.json"), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("world.json DACL is inheritable; got control %#x", control)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatalf("world.json DACL has unexpected ACEs: acl=%#v err=%v", acl, err)
	}
}
