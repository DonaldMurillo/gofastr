package desktopcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// keygen never writes a key through a symlink planted at the key or
// .pub path, live or dangling, and a refusal leaves no half pair.
func TestDesktopKeygenRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs a privilege on Windows")
	}
	for _, tc := range []struct {
		name     string
		linkPub  bool
		dangling bool
	}{
		{"live pub link", true, false},
		{"dangling pub link", true, true},
		{"live key link", false, false},
		{"dangling key link", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			key := filepath.Join(dir, "update.key")
			target := filepath.Join(dir, "elsewhere")
			if !tc.dangling {
				if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := key
			if tc.linkPub {
				link = key + ".pub"
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			_, code := verbOutput(t, func() { runDesktopKeygen([]string{"-o", key}) })
			if code != 1 {
				t.Fatalf("keygen exited %d, want 1", code)
			}
			got, err := os.ReadFile(target)
			if tc.dangling && !os.IsNotExist(err) {
				t.Fatalf("keygen created the dangling link's target: %q %v", got, err)
			}
			if !tc.dangling && string(got) != "keep" {
				t.Fatalf("keygen wrote through the link: %q", got)
			}
			if tc.linkPub {
				if _, err := os.Lstat(key); !os.IsNotExist(err) {
					t.Fatal("a refused keygen left the private key behind")
				}
			}
		})
	}
}
