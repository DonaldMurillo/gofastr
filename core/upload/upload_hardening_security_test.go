package upload_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// TestSanitize_StripsControlBytes verifies CR, LF, TAB, and other
// control bytes are removed from filenames. Logged filenames are a
// classic newline-injection surface.
func TestSanitize_StripsControlBytes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"file\rname.jpg":   "filename.jpg",
		"file\nname.jpg":   "filename.jpg",
		"file\tname.jpg":   "filename.jpg",
		"file\x1bname.jpg": "filename.jpg",
		"safe.jpg":         "safe.jpg",
	}
	for in, want := range cases {
		if got := upload.SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitize_NotHiddenAfterSanitize verifies that names made entirely
// of dots and spaces never produce a result that starts with a dot,
// leaving them as "." or "..." would still be a hidden file on POSIX.
func TestSanitize_NotHiddenAfterSanitize(t *testing.T) {
	t.Parallel()
	for _, in := range []string{". .", " . ", "...", " ... ", ".", "..", " ", "  "} {
		got := upload.SanitizeFilename(in)
		if got == "" {
			t.Errorf("SECURITY: [filename] SanitizeFilename(%q) = empty", in)
			continue
		}
		if strings.HasPrefix(got, ".") {
			t.Errorf("SECURITY: [filename] SanitizeFilename(%q) = %q (still hidden file)", in, got)
		}
	}
}

// TestLocalStorage_PartialWriteCleanup verifies that when Save fails
// mid-copy, the partial file is removed. Leaving it on disk would let
// later Get calls serve corrupt content.
func TestLocalStorage_PartialWriteCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := upload.NewLocalStorage(dir)

	// Reader that errors after a chunk to force a torn write.
	r := io.MultiReader(
		bytes.NewReader(bytes.Repeat([]byte("a"), 1024)),
		&errReader{err: errors.New("disk full")},
	)
	err := s.Save(context.Background(), "torn/file.bin", r)
	if err == nil {
		t.Fatal("expected Save to fail")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "torn", "file.bin")); !os.IsNotExist(statErr) {
		t.Errorf("SECURITY: [storage] partial file left on disk after torn write: %v", statErr)
	}
}

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

// TestLocalStorage_GetMissingScrubsPath verifies that a not-found Get
// returns ErrNotFound and does NOT include the absolute filesystem
// path in the error message.
func TestLocalStorage_GetMissingScrubsPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := upload.NewLocalStorage(dir)

	_, err := s.Get(context.Background(), "does/not/exist.bin")
	if err == nil {
		t.Fatal("expected Get to fail")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err is not os.ErrNotExist: %v", err)
	}
	if !errors.Is(err, upload.ErrNotFound) {
		t.Errorf("err is not upload.ErrNotFound: %v", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("SECURITY: [storage] error message leaks absolute path %q: %v", dir, err)
	}
}

// Pins the absolute-path leak in Delete/Exists errors, found by the
// 2026-09-04 red-probe round; fixed by routing both through resolveKey
// and scrubbing every wrap with ScrubPath.
// Property: an error returned by any LocalStorage method must not disclose the
// absolute filesystem path of the storage root or its contents. Save and
// open scrub every wrap; the same must hold for Delete and Exists, whose
// messages framework/erase_data.go and file.DeleteFileField embed verbatim
// into errors reported to operators and hosts.
// Surfaces: core/upload/local.go:Delete, core/upload/local.go:Exists.
func TestDeleteExistsLeakNoAbsPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are required to force the failing syscall deterministically")
	}
	requirePOSIXFileModes(t)
	t.Parallel()

	base := t.TempDir()
	store := upload.NewLocalStorage(base)
	ctx := context.Background()

	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// r-x: traversal and stat succeed, unlink is denied.
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(sub, 0o700) }() // let t.TempDir cleanup remove it

	// Surface 1: Delete failure must not carry the absolute path.
	err := store.Delete(ctx, "sub/f.txt")
	if err == nil {
		t.Fatal("Delete unexpectedly succeeded in a non-writable directory; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [upload-path-leak] Delete error discloses the absolute storage path: %q. Save and open scrub this exact class; erase_data.go and DeleteFileField embed this message in operator-facing errors.", err.Error())
	}

	// Surface 2: Exists failure must not carry the absolute path.
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	_, err = store.Exists(ctx, "sub/f.txt")
	if err == nil {
		t.Fatal("Exists unexpectedly succeeded under an untraversable directory; test setup is wrong")
	}
	if strings.Contains(err.Error(), base) {
		t.Errorf("SECURITY: [upload-path-leak] Exists error discloses the absolute storage path: %q, unlike Save/open.", err.Error())
	}
}

// TestLocalStorage_SaveRestrictsFilePermissions verifies uploaded files
// are not created world-readable. Multi-tenant hosts often share the
// same node; 0644 exposes user uploads to unrelated local users.
func TestLocalStorage_SaveRestrictsFilePermissions(t *testing.T) {
	requirePOSIXFileModes(t)
	t.Parallel()
	dir := t.TempDir()
	s := upload.NewLocalStorage(dir)

	if err := s.Save(context.Background(), "tenant-a/private.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "tenant-a", "private.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [storage] uploaded file permissions = %#o. Attack: local disclosure via world/group-readable upload files.", info.Mode().Perm())
	}
}

// TestLocalStorage_SaveRestrictsDirectoryPermissions verifies upload
// subdirectories are not created world-readable/executable.
func TestLocalStorage_SaveRestrictsDirectoryPermissions(t *testing.T) {
	requirePOSIXFileModes(t)
	t.Parallel()
	dir := t.TempDir()
	s := upload.NewLocalStorage(dir)

	if err := s.Save(context.Background(), "tenant-a/nested/private.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "tenant-a", "nested"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: [storage] upload directory permissions = %#o. Attack: local enumeration of tenant upload trees.", info.Mode().Perm())
	}
}
