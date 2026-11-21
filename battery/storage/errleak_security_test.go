package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
)

// Pins the absolute-storage-root leak in every LocalStorage error, found
// by the 2026-09-04 red-probe round; fixed by resolving keys through
// resolvePath (whose own errors are scrubbed) and rewriting every wrap
// to quote the key and pass syscall errors through scrubPathError.
// Property: an error returned by any LocalStorage operation must not disclose the
// absolute filesystem path of the storage root. core/upload's LocalStorage scrubs
// every wrap (ScrubPath) precisely because "the CRUD handlers echo an upload
// failure straight into a 400 body"; battery/storage is wired through the same
// documented WithFileStorage path and must hold the same invariant.
// Surfaces: battery/storage/local.go:Save (create/restrict directory, temp-file,
// rename wraps), battery/storage/local.go:open (serves Get and GetRange),
// battery/storage/local.go:Delete, battery/storage/local.go:Exists — plus
// resolvePath's own resolution errors.
func TestStorageErrorsHideAbsRoot(t *testing.T) {
	// NAME_MAX is 255 on every POSIX filesystem this battery documents; a
	// 300-byte component deterministically forces ENAMETOOLONG there.
	longComponent := strings.Repeat("a", 300)

	base := t.TempDir()
	ls := newTestLocalStorage(t, base)
	ctx := context.Background()

	// Surface 1: Save — the failure must not quote the absolute root.
	err := ls.Save(ctx, longComponent+"/f.txt", strings.NewReader("x"))
	if err == nil {
		t.Fatal("Save unexpectedly accepted a >NAME_MAX path component; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [storage-path-leak] Save error discloses the absolute storage root: %q. core/upload scrubs this exact class because CRUD echoes it into a 400 body.", err.Error())
	}

	// Surface 2+3: Get and GetRange — the error must not carry the path.
	if _, err = ls.Get(ctx, longComponent); err == nil {
		t.Fatal("Get unexpectedly accepted a >NAME_MAX path component; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [storage-path-leak] Get error discloses the absolute storage root: %q.", err.Error())
	}
	if rg, ok := storage.Storage(ls).(storage.RangeGetter); ok {
		_, err = rg.GetRange(ctx, longComponent)
		if err == nil {
			t.Fatal("GetRange unexpectedly accepted a >NAME_MAX path component; test setup is wrong")
		}
		if strings.Contains(err.Error(), base) {
			t.Errorf("SECURITY: [storage-path-leak] GetRange error discloses the absolute storage root: %q (open() is the shared enforcement point for Get/GetRange).", err.Error())
		}
	}

	// Surface 4: Delete.
	if err = ls.Delete(ctx, longComponent); err == nil {
		t.Fatal("Delete unexpectedly accepted a >NAME_MAX path component; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [storage-path-leak] Delete error discloses the absolute storage root: %q.", err.Error())
	}

	// Surface 5: Exists.
	if _, err = ls.Exists(ctx, longComponent); err == nil {
		t.Fatal("Exists unexpectedly accepted a >NAME_MAX path component; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [storage-path-leak] Exists error discloses the absolute storage root: %q.", err.Error())
	}
}

// A Save that fails on a temp dir quotes neither the storage root nor the temp dir.
func TestSaveTempDirErrorsHidePaths(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "taken", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, tempDir, key string
	}{
		{"missing temp dir under the root", filepath.Join(base, "missing"), "f.txt"},
		{"rename from an outside temp dir onto a directory", outside, "taken"},
	}
	for _, tc := range cases {
		err := newTestLocalStorage(t, base, storage.WithTempDir(tc.tempDir)).Save(context.Background(), tc.key, strings.NewReader("x"))
		if err == nil {
			t.Fatalf("%s: Save succeeded; test setup is wrong", tc.name)
		}
		if strings.Contains(err.Error(), base) || strings.Contains(err.Error(), outside) {
			t.Errorf("SECURITY: [storage-path-leak] %s: Save error quotes an absolute path: %q", tc.name, err)
		}
	}
}
