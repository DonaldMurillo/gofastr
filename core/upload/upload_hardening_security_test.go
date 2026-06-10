package upload_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
// of dots and spaces never produce a result that starts with a dot —
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

// TestLocalStorage_SaveRestrictsFilePermissions verifies uploaded files
// are not created world-readable. Multi-tenant hosts often share the
// same node; 0644 exposes user uploads to unrelated local users.
func TestLocalStorage_SaveRestrictsFilePermissions(t *testing.T) {
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
