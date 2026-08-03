//go:build windows

package log

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func requirePOSIXFileModes(t *testing.T) {
	t.Helper()
	t.Skip("POSIX permission bits are not portable on Windows")
}

func TestFileSinkWindowsACLsAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "server.log")
	sink, err := FileSink(path, FileOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("log DACL is inheritable; got control %#x", control)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatalf("log DACL has unexpected ACEs: acl=%#v err=%v", acl, err)
	}
}
