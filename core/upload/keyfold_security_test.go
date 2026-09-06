package upload_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins fold-refusal at core/upload's LocalStorage, found by the
// 2026-09-05 red-probe round (round 4); fixed by routing Save through
// RefuseFoldedKey (the shared walk battery/storage's local backend
// introduced), which refuses a byte-different spelling that the
// filesystem folds onto an existing object before anything is created.
// Property: two storage keys the backend accepted as distinct must never
// alias one object — a save through key B must not change what key A
// returns. On case-sensitive filesystems distinct spellings are distinct
// files and the property holds trivially; the probe self-skips there.

// TestFoldedKeysDoNotAlias pins the store-level fold property at
// core/upload's LocalStorage. Each shape saves key A, then saves a
// byte-different key B that the filesystem folds onto A, then requires
// that key A still returns its original bytes. Either fix shape passes:
// refusing B (battery/storage's behaviour) or keeping the objects
// distinct.
func TestFoldedKeysDoNotAlias(t *testing.T) {
	base := t.TempDir()
	ls := upload.NewLocalStorage(base)
	ctx := context.Background()

	// Probe the filesystem exactly the way the battery/storage twin
	// does: if "A"/"a" and NFC/NFD pairs do not alias, distinct
	// spellings are distinct files and the property holds trivially.
	caseFolds := false
	if err := os.WriteFile(filepath.Join(base, "red-probe-A"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "red-probe-a")); err == nil {
		caseFolds = true
	}
	normFolds := false
	nfc := "caf\u00e9-red.txt"
	nfd := "cafe\u0301-red.txt"
	if err := os.WriteFile(filepath.Join(base, nfc), []byte("x"), 0o600); err == nil {
		if _, err := os.Stat(filepath.Join(base, nfd)); err == nil {
			normFolds = true
		}
	}
	if !caseFolds && !normFolds {
		t.Skip("filesystem is case- and normalization-sensitive; distinct keys cannot alias here")
	}

	shapes := []struct {
		name string
		keyA string
		keyB string
	}{
		{"case-fold-leaf", "TenantA/report.txt", "tenanta/report.txt"},
		{"case-fold-directory", "tenants/Alpha/f.bin", "tenants/alpha/f.bin"},
	}
	if normFolds {
		shapes = append(shapes, struct{ name, keyA, keyB string }{
			"normalization-fold-leaf", "tenants/caf\u00e9.txt", "tenants/cafe\u0301.txt",
		})
	}

	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			if err := ls.Save(ctx, sh.keyA, strings.NewReader("USER-A-SECRET-CONTENT")); err != nil {
				t.Fatalf("save %q: %v", sh.keyA, err)
			}
			// The fold write itself may legitimately be refused
			// (the battery/storage fix shape); the invariant is
			// about what key A returns afterwards.
			_ = ls.Save(ctx, sh.keyB, strings.NewReader("USER-B-CONTENT"))

			rc, err := ls.Get(ctx, sh.keyA)
			if err != nil {
				t.Fatalf("get %q after folded save of %q: %v", sh.keyA, sh.keyB, err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read %q: %v", sh.keyA, err)
			}
			if string(data) != "USER-A-SECRET-CONTENT" {
				t.Errorf("SECURITY: [upload-keyfold] save through key %q changed what key %q returns: got %q, want USER-A-SECRET-CONTENT. Attack: on case-/normalization-insensitive filesystems the two keys fold onto one file, so the second writer silently overwrites the first (battery/storage refuses this via refuseFoldedKey; core/upload does not).", sh.keyB, sh.keyA, string(data))
			}
		})
	}
}
