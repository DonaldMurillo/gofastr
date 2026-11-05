package devcmd

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
)

func makeDevSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if symlinkPermissionDenied(runtime.GOOS, err) {
			t.Skipf("symlink creation is not permitted: %v", err)
		}
		t.Fatal(err)
	}
}

func symlinkPermissionDenied(goos string, err error) bool {
	return goos == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314)))
}

func TestSymlinkPermissionDenied(t *testing.T) {
	for _, test := range []struct {
		name string
		goos string
		err  error
		want bool
	}{
		{"windows privilege", "windows", &os.LinkError{Op: "symlink", Err: syscall.Errno(1314)}, true},
		{"windows permission", "windows", &os.LinkError{Op: "symlink", Err: os.ErrPermission}, true},
		{"windows permission wrapped", "windows", errors.Join(errors.New("fixture"), os.ErrPermission), true},
		{"windows missing path", "windows", &os.LinkError{Op: "symlink", Err: os.ErrNotExist}, false},
		{"windows unexpected failure", "windows", errors.New("unexpected failure"), false},
		{"windows successful creation", "windows", nil, false},
		{"unix permission", "linux", os.ErrPermission, false},
		{"mac permission", "darwin", os.ErrPermission, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := symlinkPermissionDenied(test.goos, test.err); got != test.want {
				t.Errorf("permission skip = %v; want %v for %v", got, test.want, test.err)
			}
		})
	}
}
