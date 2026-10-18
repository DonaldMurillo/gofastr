package filepermtest

import (
	"os"
	"runtime"
	"testing"
)

// AssertMode checks Unix mode bits, or the read-only attribute exposed by Go on Windows.
// Use AssertOwnerOnly for secret files, whose Windows permissions require an ACL check.
func AssertMode(t testing.TB, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got := info.Mode().Perm()
	if runtime.GOOS == "windows" {
		got, want = got&0o200, want&0o200
	}
	if got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
