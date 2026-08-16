package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Property: the scaffolded .env is never readable by anyone but its owner.
//
// It carries DATABASE_URL and is the documented home of GOFASTR_SECRET, the
// session-signing key.
//
// The pre-existing case is the one that matters: os.WriteFile applies its
// mode only when it CREATES a file, so `gofastr init .` in a directory that
// already holds a 0644 .env would have truncated it, written the secrets in,
// and left it world-readable — the permission argument silently doing
// nothing on exactly the path where it was needed.
func TestEnvFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	for _, tc := range []struct {
		name    string
		preMode os.FileMode // 0 = no pre-existing file
	}{
		{"new file", 0},
		{"pre-existing world-readable", 0o644},
		{"pre-existing group-writable", 0o664},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tc.preMode != 0 {
				if err := os.WriteFile(path, []byte("STALE=1\n"), tc.preMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, tc.preMode); err != nil {
					t.Fatal(err) // defeat umask so the precondition is exact
				}
			}

			if err := writeEnvFile(path, "GOFASTR_SECRET=shhh\n"); err != nil {
				t.Fatalf("writeEnvFile: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got&0o077 != 0 {
				t.Errorf("SECURITY: .env is %04o — readable beyond its owner", got)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "GOFASTR_SECRET=shhh\n" {
				t.Errorf("content = %q, want the new content with no stale remnant", body)
			}
		})
	}
}
