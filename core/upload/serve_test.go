package upload

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mountServe mounts ServeHandler on a stdlib ServeMux at
// /uploads/{key...} so tests exercise real PathValue wildcard
// resolution, exactly how the framework router mounts it.
func mountServe(t *testing.T, storage Storage) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/uploads/{key...}", ServeHandler(storage))
	return mux
}

func saveKey(t *testing.T, storage Storage, key, content string) {
	t.Helper()
	if err := storage.Save(context.Background(), key, strings.NewReader(content)); err != nil {
		t.Fatalf("Save(%q): %v", key, err)
	}
}

// TestServeHandlerBlocksTraversal proves path-traversal defense in the
// Storage backend (sanitizeKey) holds through the HTTP handler: keys
// with ../, backslashes, and encoded slashes never read a file outside
// baseDir. The handler performs NO path logic itself — it only
// classifies the typed error the backend returns.
func TestServeHandlerBlocksTraversal(t *testing.T) {
	t.Parallel()

	// Two sibling trees under a private root: uploads (the baseDir) and
	// a secret file that lives strictly OUTSIDE it.
	root := t.TempDir()
	baseDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretDir, 0o750); err != nil {
		t.Fatal(err)
	}
	secret := "TRAVERSAL_SECRET_" + filepath.Base(root)
	if err := os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	storage := NewLocalStorage(baseDir)
	saveKey(t, storage, "users/avatar/a.png", "png-body")

	for _, key := range []string{
		"../secrets/secret.txt",
		"..\\secrets\\secret.txt",
		"..%2f..%2fsecrets/secret.txt",
		"/etc/passwd",
	} {
		req := httptest.NewRequest(http.MethodGet, "/uploads/x", nil)
		req.SetPathValue("key", key)
		rr := httptest.NewRecorder()
		ServeHandler(storage).ServeHTTP(rr, req)

		if rr.Code < 400 || rr.Code >= 500 {
			t.Errorf("key=%q: status=%d, want 4xx", key, rr.Code)
		}
		if strings.Contains(rr.Body.String(), secret) {
			t.Errorf("SECURITY: key=%q leaked out-of-tree contents: %q", key, rr.Body.String())
		}
	}
}

// TestServeHandlerMissingKey404 verifies a missing key maps to 404 via
// ErrNotFound and never echoes a filesystem path.
func TestServeHandlerMissingKey404(t *testing.T) {
	t.Parallel()
	storage := NewLocalStorage(t.TempDir())
	h := mountServe(t, storage)

	req := httptest.NewRequest(http.MethodGet, "/uploads/does-not-exist.png", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rr.Code)
	}
	if strings.Contains(rr.Body.String(), t.TempDir()) {
		t.Errorf("SECURITY: 404 body leaks temp dir: %q", rr.Body.String())
	}
}

// TestServeHandlerSniffsContentType verifies the content type is sniffed
// from the body (not derived from the extension or client) and that
// nosniff is always set.
func TestServeHandlerSniffsContentType(t *testing.T) {
	t.Parallel()
	storage := NewLocalStorage(t.TempDir())
	// PNG signature so http.DetectContentType returns image/png even
	// though the extension is irrelevant to the handler.
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	saveKey(t, storage, "img/a.png", png)

	h := mountServe(t, storage)
	req := httptest.NewRequest(http.MethodGet, "/uploads/img/a.png", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("Content-Type=%q, want image/png", ct)
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error(`missing X-Content-Type-Options: nosniff`)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("\x89PNG")) {
		t.Error("body should contain the full sniffed payload")
	}
}

// TestServeHandlerNeutralizesHTML verifies stored-XSS defense: an
// uploaded HTML or SVG body is never served as a browser-renderable
// type — it is forced to application/octet-stream + attachment.
func TestServeHandlerNeutralizesHTML(t *testing.T) {
	t.Parallel()
	storage := NewLocalStorage(t.TempDir())
	htmlBody := "<html><body><script>alert('xss')</script></body></html>"
	saveKey(t, storage, "evil.html", htmlBody)
	svgBody := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	saveKey(t, storage, "evil.svg", svgBody)

	h := mountServe(t, storage)
	for _, tc := range []struct{ path string }{
		{"/uploads/evil.html"},
		{"/uploads/evil.svg"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200", tc.path, rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("%s: Content-Type=%q, want application/octet-stream", tc.path, ct)
		}
		if cd := rr.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
			t.Errorf("%s: Content-Disposition=%q, want attachment", tc.path, cd)
		}
	}
}

// TestServeHandlerMethodNotAllowed verifies non-GET/HEAD methods return
// 405 with an Allow header.
func TestServeHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := ServeHandler(NewLocalStorage(t.TempDir()))

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	} {
		req := httptest.NewRequest(method, "/uploads/x", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status=%d, want 405", method, rr.Code)
		}
		if got := rr.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s: Allow=%q, want \"GET, HEAD\"", method, got)
		}
	}
}
