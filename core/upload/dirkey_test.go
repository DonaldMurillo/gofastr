package upload_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// A key naming a directory of the store (the uploads/<entity>/<field>
// prefixes every saved object creates) is not an object. Get opened the
// directory and handed back its handle, so ServeHandler's first read
// failed and every GET of a prefix was a 500, and Exists reported the
// prefix as a stored object. Property: only a regular file is an object;
// a directory key is not found.
func TestDirectoryKeyIsNotAnObject(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := upload.NewLocalStorage(dir)
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
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/"+key, nil)
		req.SetPathValue("key", key)
		upload.ServeHandler(s)(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /%s = %d, want 404", key, rec.Code)
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
		if err := s.Delete(ctx, key); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Delete(%q) = %v, want os.ErrNotExist", key, err)
		}
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil || !fi.IsDir() {
			t.Errorf("Delete(%q) removed the prefix: %v", key, err)
		}
	}
}
