package upload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins symlink-escape refusal at every LocalStorage surface, found by the
// 2026-09-04 red-probe round; fixed by routing Save, open (Get/GetRange),
// Delete, and Exists through resolveKey → ResolveUnderRoot, which proves
// containment on the symlink-resolved chain (leaf included) before any
// filesystem call.
// Property: a storage key that resolves INSIDE the base directory lexically but
// through a symlinked directory — or to a symlinked leaf — must be refused at
// every surface: write, read, range-read, existence check, and delete.
// Surfaces: core/upload/local.go:Save, core/upload/local.go:open (serves Get
// and GetRange), core/upload/local.go:Delete, core/upload/local.go:Exists —
// all behind resolveKey, the single enforcement point. The sibling contract is
// framework/contracts TestApplyRefusesSymlinkEscape; battery/storage's
// LocalStorage holds the same line through the shared ResolveUnderRoot.
func TestLocalStorageRefusesSymlinkEscape(t *testing.T) {
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
	// Same invariant, leaf shape: a symlinked FILE, not just a symlinked
	// directory. EvalSymlinks resolves the full existing path, so the leaf
	// link must be refused on exactly the same terms.
	leafSecret := filepath.Join(outside, "leaf-secret.txt")
	if err := os.WriteFile(leafSecret, []byte("leaf-outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(leafSecret, filepath.Join(base, "leaf-link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	store := upload.NewLocalStorage(base)
	ctx := context.Background()

	// Sweep: every surface, both escape shapes. A surface that follows
	// the link instead of refusing it is the finding this test pins.
	cases := []struct {
		key  string
		note string
	}{
		{"link/secret.txt", "symlinked directory"},
		{"leaf-link", "symlinked leaf"},
	}

	for _, tc := range cases {
		// Get and GetRange must refuse the key with ErrInvalidKey.
		rc, err := store.Get(ctx, tc.key)
		if err == nil {
			buf := make([]byte, 64)
			n, _ := rc.Read(buf)
			rc.Close()
			t.Errorf("SECURITY: [upload-symlink-read] Get(%q) succeeded through a %s and served %d byte(s) from OUTSIDE the storage root (%q begins %q).", tc.key, tc.note, n, secret, buf[:min(n, 32)])
		} else if !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Get(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
		}
		if rg, ok := upload.Storage(store).(upload.RangeGetter); ok {
			rs, err := rg.GetRange(ctx, tc.key)
			if err == nil {
				rs.Close()
				t.Errorf("SECURITY: [upload-symlink-read] GetRange(%q) opened a file outside the storage root through a %s; open() is the shared enforcement point for Get and GetRange.", tc.key, tc.note)
			} else if !errors.Is(err, upload.ErrInvalidKey) {
				t.Errorf("GetRange(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
			}
		}

		// Exists must not report objects outside the root.
		if ok, err := store.Exists(ctx, tc.key); err == nil && ok {
			t.Errorf("SECURITY: [upload-symlink-exists] Exists(%q) reported true for a file OUTSIDE the storage root through a %s: an existence oracle for arbitrary paths under the symlink target.", tc.key, tc.note)
		} else if err != nil && !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("Exists(%q) through a %s: error is %v, want an ErrInvalidKey refusal", tc.key, tc.note, err)
		}

		// Delete must not unlink outside the root.
		if err := store.Delete(ctx, tc.key); err == nil {
			t.Errorf("SECURITY: [upload-symlink-delete] Delete(%q) succeeded through a %s; only a path inside the root may be unlinked.", tc.key, tc.note)
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
		if err := store.Save(ctx, key, strings.NewReader("escaped")); err == nil {
			t.Errorf("SECURITY: [upload-symlink-write] Save(%q) accepted a key that resolves outside the storage root.", key)
		}
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); statErr == nil {
		t.Errorf("SECURITY: [upload-symlink-write] Save(\"link/evil.txt\") wrote a file OUTSIDE the storage root via the symlinked directory.")
	}
}
