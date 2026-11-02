package initcmd

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
)

// Property: the scaffolded .env is never readable by anyone but its owner.
//
// It carries DATABASE_URL and is the documented home of GOFASTR_SECRET, the
// session-signing key.
//
// The pre-existing case is the one that matters: os.WriteFile applies its
// mode only when it CREATES a file, so `gofastr init .` in a directory that
// already holds a 0644 .env would have truncated it, written the secrets in,
// and left it world-readable, the permission argument silently doing
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

			if err := writeEnvFile(filepath.Dir(path), "GOFASTR_SECRET=shhh\n"); err != nil {
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

// `init . --force` truncated .env and the scaffold files in place: a
// crash or a full disk mid-write left them empty. Each is renamed over
// whole, and a refreshed file keeps its mode.
func TestInitRewritesReplaceWhole(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	gi := filepath.Join(dir, "main.go")
	for _, p := range []string{env, gi} {
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldEnv, oldMain := heldInit(t, env), heldInit(t, gi)
	if err := writeEnvFile(dir, "NEW=1\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeScaffoldFile(dir, "main.go", []byte("package main\n")); err != nil {
		t.Fatal(err)
	}
	if a, b := oldEnv(), oldMain(); a != "old" || b != "old" {
		t.Fatalf("old handles read .env %q, main.go %q: rewritten in place", a, b)
	}
	cfg := filepath.Join(dir, settings.FileName)
	if err := os.WriteFile(cfg, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCfg := heldInit(t, cfg)
	writeSettingsFile(dir, "sqlite")
	if got := oldCfg(); got != "old" {
		t.Fatalf("old handle reads %s %q: rewritten in place", settings.FileName, got)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(gi); fi.Mode().Perm() != 0o600 {
			t.Fatalf("main.go mode %v, want its 0600 kept", fi.Mode().Perm())
		}
	}
}

// heldInit opens path and returns a reader of what that handle sees: a
// file renamed over path leaves it reading the old content, one
// truncated in place leaves it reading the new.
func heldInit(t *testing.T, path string) func() string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return func() string {
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}
