package storage_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins that LocalStorage.Save refuses a key that folds onto an existing
// object stored under a byte-different spelling, found by the 2026-09-04
// red-probe round; fixed by refuseFoldedKey, which walks every path
// component of the resolved destination (Lstat the component as spelled,
// then os.SameFile-match the parent's actual directory entries) and
// refuses with an upload.ErrInvalidKey error before anything is created
// or written.
// Property: two keys the store accepted as distinct must not alias one
// object — a save through key B must never change what key A returns. On
// case-insensitive/normalization-insensitive filesystems (macOS default
// APFS, most CIFS mounts) the filesystem itself folds such keys to one
// file, so the store is the layer that has to keep them distinct: it
// refuses the second spelling instead of overwriting. Objects written by
// other tools directly into BaseDir with folded spellings are still the
// filesystem's behaviour, not the store's.
// Surfaces: battery/storage/local.go::Save (the only write surface; the
// guard runs before both the os.Root path and the saveResolveThenOpen
// fallback, and covers DIRECTORY components too, so "tenanta/new.txt"
// cannot be planted inside an existing "TenantA/" namespace). The memory
// and S3 backends key on the exact string and cannot alias; Get/Exists/
// Delete only observe a fold a write already created.
// Skipped automatically where the filesystem is case- and
// normalization-sensitive (Linux ext4 & friends): distinct spellings are
// distinct files there and the property holds trivially.
func TestCaseFoldKeysStayDistinct(t *testing.T) {
	base := t.TempDir()
	ls := storage.NewLocalStorage(base)
	ctx := context.Background()

	// Probe the filesystem: if "A"/"a" and NFC/NFD name pairs do NOT
	// alias, the property holds trivially and there is nothing to prove
	// here.
	probeA := filepath.Join(base, "red-probe-A")
	if err := os.WriteFile(probeA, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	caseFolds := false
	if _, err := os.Stat(filepath.Join(base, "red-probe-a")); err == nil {
		caseFolds = true
	}
	nfc := "caf\u00e9-red.txt"  // NFC: e-acute composed
	nfd := "cafe\u0301-red.txt" // NFD: e + combining acute
	normFolds := false
	if err := os.WriteFile(filepath.Join(base, nfc), []byte("x"), 0o600); err == nil {
		if _, err := os.Stat(filepath.Join(base, nfd)); err == nil {
			normFolds = true
		}
	}
	if !caseFolds && !normFolds {
		t.Skip("filesystem is case- and normalization-sensitive; distinct keys cannot alias here")
	}

	// Shape 1: case-only difference (the Foo.png vs foo.png overwrite).
	if caseFolds {
		if err := ls.Save(ctx, "TenantA/report.txt", strings.NewReader("content-A")); err != nil {
			t.Fatalf("Save A: %v", err)
		}
		err := ls.Save(ctx, "tenanta/report.txt", strings.NewReader("content-B"))
		if err == nil {
			t.Fatalf("SECURITY: [storage-case-collide] Save(\"tenanta/report.txt\") was accepted after " +
				"\"TenantA/report.txt\": on this filesystem the two keys alias one file, so the save " +
				"must be refused, not silently overwrite the other key's object")
		}
		if !errors.Is(err, upload.ErrInvalidKey) {
			t.Errorf("collision refusal should classify as upload.ErrInvalidKey (serve layer maps it "+
				"to 400), got: %v", err)
		}
		rc, err := ls.Get(ctx, "TenantA/report.txt")
		if err != nil {
			t.Fatalf("Get after refused save: %v", err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		if string(body) != "content-A" {
			t.Errorf("SECURITY: [storage-case-collide] refused save still mutated the object: "+
				"Get(\"TenantA/report.txt\") = %q", string(body))
		}

		// Directory component folds too: a new object must not be planted
		// inside the other spelling's namespace.
		if err := ls.Save(ctx, "tenanta/other.txt", strings.NewReader("content-C")); err == nil {
			t.Errorf("SECURITY: [storage-case-collide] Save(\"tenanta/other.txt\") was accepted inside " +
				"the folded \"TenantA/\" directory: the collision must be refused at the DIRECTORY " +
				"component, not only at the leaf")
		}

		// The guard must not overreach: re-saving the exact stored key
		// stays an ordinary overwrite.
		if err := ls.Save(ctx, "TenantA/report.txt", strings.NewReader("content-A2")); err != nil {
			t.Fatalf("exact-key re-save must stay allowed: %v", err)
		}
		rc, err = ls.Get(ctx, "TenantA/report.txt")
		if err != nil {
			t.Fatal(err)
		}
		body, _ = io.ReadAll(rc)
		rc.Close()
		if string(body) != "content-A2" {
			t.Errorf("exact-key re-save did not overwrite: %q", string(body))
		}
	}

	// Shape 2: Unicode-normalization-only difference.
	if normFolds {
		if err := ls.Save(ctx, "dir/"+nfc, strings.NewReader("nfc-content")); err != nil {
			t.Fatalf("Save NFC: %v", err)
		}
		if err := ls.Save(ctx, "dir/"+nfd, strings.NewReader("nfd-content")); err == nil {
			t.Fatalf("SECURITY: [storage-norm-collide] Save under the NFD spelling of an NFC-stored key " +
				"was accepted: the two spellings alias one file on this filesystem and the save must " +
				"be refused")
		}
		rc, err := ls.Get(ctx, "dir/"+nfc)
		if err != nil {
			t.Fatalf("Get NFC after refused save: %v", err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		if string(body) != "nfc-content" {
			t.Errorf("SECURITY: [storage-norm-collide] refused NFD save still mutated the NFC object: %q",
				string(body))
		}
	}
}
