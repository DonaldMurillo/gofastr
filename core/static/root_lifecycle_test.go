package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerReleasesRootBetweenRequests(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "public")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{FS: os.DirFS(dir)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "original" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	// Renaming the original keeps its identity alive while testing a replacement.
	if err := os.Rename(dir, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("replaced root response = %d %q", w.Code, w.Body.String())
	}
	if err := os.RemoveAll(filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
}
