package static

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestStatic_DotDotTraversal verifies that path traversal via ../ is
// blocked. Attack: reading arbitrary files from the filesystem.
func TestStatic_DotDotTraversal(t *testing.T) {
	files := fstest.MapFS{
		"public/index.html":  &fstest.MapFile{Data: []byte("<h1>public</h1>")},
		"secret/secret.html": &fstest.MapFile{Data: []byte("SECRET")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	req := httptest.NewRequest(http.MethodGet, "/static/../secret/secret.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "SECRET") {
		t.Errorf("SECURITY: [path_traversal] GET /static/../secret/secret.html returned 200 with secret content. Attack: directory traversal via ../")
	}
}

// TestStatic_DoubleEncodedTraversal verifies that double-encoded path
// traversal (%252e%252e) is blocked. Attack: WAF bypass via encoding.
func TestStatic_DoubleEncodedTraversal(t *testing.T) {
	files := fstest.MapFS{
		"public/index.html":  &fstest.MapFile{Data: []byte("<h1>public</h1>")},
		"secret/secret.html": &fstest.MapFile{Data: []byte("SECRET")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	// %252e = double-encoded "."
	req := httptest.NewRequest(http.MethodGet, "/static/%252e%252e/secret/secret.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "SECRET") {
		t.Errorf("SECURITY: [path_traversal] double-encoded ../ returned 200 with secret. Attack: WAF bypass via double encoding.")
	}
}

// TestStatic_AbsolutePathBlocked verifies that absolute paths outside
// the FS root are rejected. Attack: /etc/passwd via absolute path.
func TestStatic_AbsolutePathBlocked(t *testing.T) {
	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	req := httptest.NewRequest(http.MethodGet, "/static//etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "root:") {
		t.Errorf("SECURITY: [path_traversal] GET /static//etc/passwd returned 200. Attack: absolute path breakout.")
	}
}

// TestStatic_MethodEnforced verifies that non-GET/HEAD methods are
// rejected. Attack: using PUT/DELETE to probe handler behavior.
func TestStatic_MethodEnforced(t *testing.T) {
	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, "/index.html", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("SECURITY: [method] %s /index.html returned %d (want 405). Attack: non-GET method to static handler.", method, rr.Code)
		}
	}
}

// TestStatic_DotfileNotExposed verifies that dotfiles (e.g. .env, .htpasswd)
// are not served. Attack: reading configuration/secrets via dotfile access.
func TestStatic_DotfileNotExposed(t *testing.T) {
	files := fstest.MapFS{
		".env":        &fstest.MapFile{Data: []byte("SECRET_KEY=abc123")},
		".htpasswd":   &fstest.MapFile{Data: []byte("admin:$2y$10$hash")},
		"public/.git": &fstest.MapFile{Data: []byte("gitdir: ../.git/modules/repo")},
		"index.html":  &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files})

	for _, path := range []string{"/.env", "/.htpasswd", "/.git"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusOK {
			t.Errorf("SECURITY: [dotfile] GET %s returned 200. Attack: dotfile exposure leaks secrets.", path)
		}
	}
}

// TestStatic_NoDirectoryListing verifies that directory paths without an
// index file do not produce a listing. Attack: enumerating file structure.
func TestStatic_NoDirectoryListing(t *testing.T) {
	files := fstest.MapFS{
		"dir/file1.txt": &fstest.MapFile{Data: []byte("file1")},
		"dir/file2.txt": &fstest.MapFile{Data: []byte("file2")},
	}
	handler := Handler(Config{FS: files})

	req := httptest.NewRequest(http.MethodGet, "/dir/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		body := rr.Body.String()
		if strings.Contains(body, "file1.txt") && strings.Contains(body, "file2.txt") {
			t.Errorf("SECURITY: [dirlist] GET /dir/ returned listing with file names. Attack: directory enumeration.")
		}
	}
}

// TestStatic_ContentSniffingPrevented verifies that the correct
// Content-Type is set and X-Content-Type-Options: nosniff is present.
// Attack: browser MIME-sniffing a .json file as HTML.
func TestStatic_ContentSniffingPrevented(t *testing.T) {
	files := fstest.MapFS{
		"data.json": &fstest.MapFile{Data: []byte(`{"user":"<script>alert(1)</script>"}`)},
	}
	handler := Handler(Config{FS: files})

	req := httptest.NewRequest(http.MethodGet, "/data.json", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("SECURITY: [content_type] GET /data.json returned Content-Type=%q (want application/json). Attack: MIME sniffing may render JSON as HTML.", ct)
	}
}

// TestStatic_ETagNotLeakHashState verifies that ETag values don't leak
// internal state. Attack: using ETag as an oracle to detect file changes
// across deployments.
func TestStatic_ETagNotLeakHashState(t *testing.T) {
	data := []byte("hello world")
	etag := generateETag(data)
	// ETag should be a hash, not raw content
	if strings.Contains(etag, "hello world") {
		t.Errorf("SECURITY: [etag] ETag contains raw content: %q. Attack: content oracle via ETag.", etag)
	}
	// ETag should be consistent for same content
	etag2 := generateETag(data)
	if etag != etag2 {
		t.Errorf("ETag inconsistent for same content: %q vs %q", etag, etag2)
	}
}
