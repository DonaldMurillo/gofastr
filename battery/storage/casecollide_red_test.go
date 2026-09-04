//go:build red

package storage_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
)

// CONTRACT-QUESTION red: must distinct storage keys that differ ONLY by ASCII case or by
// Unicode normalization form stay distinct objects? On the default macOS filesystem
// (case-insensitive, normalization-insensitive APFS) they alias to one file today, so
// Save("tenantone/report.txt") silently overwrites TenantOne's object and Get returns the
// other tenant's bytes. The maintainer must either (a) make LocalStorage detect the
// collision at Save time (a resolved-path/SameFile probe) and refuse, or (b) document that
// key distinctness is the filesystem's case/normalization behaviour, not the store's
// contract. No doc or sibling pin decides this today; core/upload sidesteps it by making
// every key server-generated with a crypto/rand suffix, which is why nothing caught it.
//
// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F3 Path canonicalization at filesystem sinks (case + Unicode-normalization
// collisions at the filesystem sink; the brief's Foo.png vs foo.png shape)
// Property: two keys the store accepted as distinct must not alias one object — an
// overwrite through key B must never change what key A returns.
// Surfaces: battery/storage/local.go:Save + open (Get/GetRange share it) + Exists.
// Finding: on case-insensitive/normalizing filesystems (macOS default APFS),
// Save("tenanta/report.txt") overwrites the object stored under "TenantA/report.txt",
// and Get("TenantA/...") returns the second writer's bytes. Observed by running this test
// (skipped automatically where the filesystem is case- and normalization-sensitive).
// Severity: medium — host apps build keys from tenant/user identifiers; on macOS dev and
// single-node prod deployments two identifiers differing by case or normalization form
// share one object.
// Fix direction: at Save time, Lstat the resolved path and refuse when it exists under a
// byte-different key (or resolve with the filesystem's own folding), or document the
// filesystem-dependence as the contract.

func TestCaseFoldKeysStayDistinct(t *testing.T) {
	base := t.TempDir()
	ls := storage.NewLocalStorage(base)
	ctx := context.Background()

	// Probe the filesystem: if "A"/"a" and NFC/NFD name pairs do NOT alias, the
	// property holds trivially and there is nothing to prove here.
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
		if err := ls.Save(ctx, "tenanta/report.txt", strings.NewReader("content-B")); err != nil {
			t.Fatalf("Save B: %v", err)
		}
		rc, err := ls.Get(ctx, "TenantA/report.txt")
		if err != nil {
			t.Fatalf("Get after both saves: %v", err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		if string(body) != "content-A" {
			t.Errorf("SECURITY: [storage-case-collide] Save(\"tenanta/report.txt\") overwrote the object stored under \"TenantA/report.txt\": Get(\"TenantA/report.txt\") = %q. Two keys the store accepted as distinct alias one file on this filesystem, so identifiers differing only by case share (and clobber) one object.", string(body))
		}
	}

	// Shape 2: Unicode-normalization-only difference.
	if normFolds {
		if err := ls.Save(ctx, "dir/"+nfc, strings.NewReader("nfc-content")); err != nil {
			t.Fatalf("Save NFC: %v", err)
		}
		if err := ls.Save(ctx, "dir/"+nfd, strings.NewReader("nfd-content")); err != nil {
			t.Fatalf("Save NFD: %v", err)
		}
		rc, err := ls.Get(ctx, "dir/"+nfc)
		if err != nil {
			t.Fatalf("Get NFC after both saves: %v", err)
		}
		body, _ := io.ReadAll(rc)
		rc.Close()
		if string(body) != "nfc-content" {
			t.Errorf("SECURITY: [storage-norm-collide] Save under the NFD form overwrote the object stored under the NFC form: Get(NFC key) = %q. Keys differing only by Unicode normalization alias one file on this filesystem.", string(body))
		}
	}
}
