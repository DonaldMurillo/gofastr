//go:build windows

package contractstest

import (
	"golang.org/x/sys/windows"
	"testing"
)

// ObstructApply keeps source readable but blocks writes until cleanup.
// A read-only sharing mode refuses even an administrator's second write
// handle, without depending on symlink privileges or permission bits.
func ObstructApply(t testing.TB, path string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Error(err)
		}
	})
}
