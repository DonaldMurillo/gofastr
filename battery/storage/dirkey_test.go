package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// The twin of core/upload's TestDirectoryKeyIsNotAnObject: a key naming
// a directory of the store is a prefix, not an object, so Get, GetRange
// and Exists report it not found instead of handing back the directory.
func TestLocalDirectoryKeyIsNotAnObject(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := NewLocalStorage(dir)
	t.Cleanup(func() { s.Close() })
	if err := s.Save(ctx, "uploads/posts/a.png", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"uploads", "uploads/posts"} {
		if _, err := s.Get(ctx, key); !errors.Is(err, upload.ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", key, err)
		}
		if _, err := s.GetRange(ctx, key); !errors.Is(err, upload.ErrNotFound) {
			t.Errorf("GetRange(%q) = %v, want ErrNotFound", key, err)
		}
		if ok, err := s.Exists(ctx, key); ok || err != nil {
			t.Errorf("Exists(%q) = %v, %v; want false, nil", key, ok, err)
		}
	}
	// Delete honours the same property: a directory key is not found,
	// and the prefix stays. Delete removed an empty prefix and reported
	// success, and failed a non-empty one with an error that was not
	// not-found, so erasure counted it a hard failure.
	if err := s.Save(ctx, "uploads/empty/b.png", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "uploads/empty/b.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "uploads", "empty")); err != nil {
		t.Fatalf("setup: the emptied prefix is gone: %v", err)
	}
	for _, key := range []string{"uploads/empty", "uploads/posts"} {
		if err := s.Delete(ctx, key); err != nil {
			t.Errorf("Delete(%q) = %v, want nil", key, err)
		}
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil || !fi.IsDir() {
			t.Errorf("Delete(%q) removed the prefix: %v", key, err)
		}
	}
}
