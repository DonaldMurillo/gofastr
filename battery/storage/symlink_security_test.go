package storage_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins symlink-escape refusal at every battery/storage LocalStorage
// surface, found by the 2026-09-04 red-probe round; fixed by replacing
// the lexical-only fullPath check with resolvePath, which proves
// containment on the symlink-resolved chain via the shared
// upload.ResolveUnderRoot before any filesystem call.
// Property: storage-root containment must be enforced on the RESOLVED path at every
// filesystem sink. validateKey checks the key lexically only; a symlinked
// directory — or a symlinked leaf — inside BaseDir must not funnel reads,
// writes, existence checks, or deletes outside the root.
// Surfaces: battery/storage/local.go:Save, battery/storage/local.go:open (serves
// Get and GetRange), battery/storage/local.go:Delete,
// battery/storage/local.go:Exists — all behind resolvePath. core/upload's
// LocalStorage holds the same line through the same helper
// (TestLocalStorageRefusesSymlinkEscape).
func TestLocalStorageSymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink planting and POSIX containment semantics are not portable on Windows")
	}

	base := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Same invariant, leaf shape: a symlinked FILE must be refused on
	// exactly the same terms as a symlinked directory.
	leafSecret := filepath.Join(outside, "leaf-secret.txt")
	if err := os.WriteFile(leafSecret, []byte("leaf-outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(leafSecret, filepath.Join(base, "leaf-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	ls := storage.NewLocalStorage(base)
	ctx := context.Background()

	// Sweep: every read/stat/delete surface, both escape shapes.
	cases := []struct {
		key  string
		note string
	}{
		{"link/secret.txt", "symlinked directory"},
		{"leaf-link", "symlinked leaf"},
	}
	for _, tc := range cases {
		rc, err := ls.Get(ctx, tc.key)
		if err == nil {
			body, _ := io.ReadAll(rc)
			rc.Close()
			if strings.Contains(string(body), "outside") {
				t.Errorf("SECURITY: [storage-symlink-read] Get(%q) served %d byte(s) of a file OUTSIDE BaseDir through a %s.", tc.key, len(body), tc.note)
			}
		} else if !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Get(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
		}
		if rg, ok := storage.Storage(ls).(storage.RangeGetter); ok {
			rs, err := rg.GetRange(ctx, tc.key)
			if err == nil {
				body, _ := io.ReadAll(rs)
				rs.Close()
				if strings.Contains(string(body), "outside") {
					t.Errorf("SECURITY: [storage-symlink-read] GetRange(%q) served %d byte(s) from outside BaseDir through a %s; open() is the shared enforcement point.", tc.key, len(body), tc.note)
				}
			} else if !errors.Is(err, upload.ErrInvalidKey) {
				t.Errorf("GetRange(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
			}
		}
		if ok, err := ls.Exists(ctx, tc.key); err == nil && ok {
			t.Errorf("SECURITY: [storage-symlink-exists] Exists(%q) reported true for a file OUTSIDE BaseDir through a %s: an existence oracle for arbitrary paths under the symlink target.", tc.key, tc.note)
		} else if err != nil && !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Exists(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
		}
		if err := ls.Delete(ctx, tc.key); err == nil {
			t.Errorf("SECURITY: [storage-symlink-delete] Delete(%q) succeeded through a %s; only a path inside BaseDir may be unlinked.", tc.key, tc.note)
		}
	}

	// Neither outside target may have been touched by any surface above.
	for _, target := range []string{secret, leafSecret} {
		if _, statErr := os.Stat(target); statErr != nil {
			t.Fatalf("outside target %s vanished; a surface mutated outside the root", target)
		}
	}

	// Save must not land outside the root through either shape.
	for _, key := range []string{"link/evil.txt", "leaf-link"} {
		if err := ls.Save(ctx, key, strings.NewReader("escaped")); err == nil {
			t.Errorf("SECURITY: [storage-symlink-write] Save(%q) accepted a key that resolves outside BaseDir.", key)
		}
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
		t.Errorf("SECURITY: [storage-symlink-write] Save(\"link/evil.txt\") wrote a file OUTSIDE BaseDir via the symlinked directory.")
	}
}
