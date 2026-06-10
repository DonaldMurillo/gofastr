package static

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/*
var testFS embed.FS

// testSubFS returns a sub-filesystem rooted at "testdata".
func testSubFS() fs.FS {
	sub, _ := fs.Sub(testFS, "testdata")
	return sub
}

func TestServeFileWithCorrectContentType(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantType    string
		wantContent string
	}{
		{"html file", "/hello.html", "text/html", "<h1>Hello</h1>"},
		{"css file", "/style.css", "text/css", "body { color: red; }"},
		{"js file", "/app.js", "text/javascript", "console.log("},
		{"json file", "/data.json", "application/json", `{"key": "value"}`},
		{"svg file", "/logo.svg", "image/svg+xml", "<svg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Handler(Config{
				FS:     testSubFS(),
				Prefix: "/static",
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d; body: %s", rec.Code, rec.Body.String())
			}

			ct := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, tt.wantType) {
				t.Errorf("Content-Type = %q, want prefix %q", ct, tt.wantType)
			}

			if !strings.Contains(rec.Body.String(), tt.wantContent) {
				t.Errorf("body = %q, want to contain %q", rec.Body.String(), tt.wantContent)
			}
		})
	}
}

func TestETagHeader(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set")
	}

	// ETag should be double-quoted.
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag = %q, expected double-quoted value", etag)
	}

	// Same file should produce same ETag.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	etag2 := rec2.Header().Get("ETag")
	if etag != etag2 {
		t.Errorf("ETag mismatch: first=%q second=%q", etag, etag2)
	}
}

func Test304ForMatchingIfNoneMatch(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	// First request to get ETag.
	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set")
	}

	// Second request with If-None-Match.
	req2 := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", rec2.Code)
	}

	if rec2.Body.Len() > 0 {
		t.Errorf("expected empty body for 304, got %q", rec2.Body.String())
	}
}

func Test304ForWildcardIfNoneMatch(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected status 304 for wildcard If-None-Match, got %d", rec.Code)
	}
}

func Test404ForMissingFiles(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
		SPA:    true,
	})

	// Request a non-existent path that looks like a client-side route.
	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 with SPA fallback, got %d; body: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, expected text/html for SPA fallback", ct)
	}

	// Should serve index.html content.
	if !strings.Contains(rec.Body.String(), "Index") {
		t.Errorf("expected index.html content, got %q", rec.Body.String())
	}
}

func TestSPAFallbackDisabled(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
		SPA:    false,
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 without SPA, got %d", rec.Code)
	}
}

func TestDirectoryListingDisabledByDefault(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	// Request the root — should serve index.html, not a listing.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// With DirListing=false and an index.html present, it should serve the index.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for index.html, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, expected text/html", ct)
	}
}

func TestDirectoryTraversalBlocked(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	tests := []string{
		"/../secret.txt",
		"/../../etc/passwd",
		"/static/../../../etc/passwd",
	}

	for _, p := range tests {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("expected 404 for traversal path %q, got %d", p, rec.Code)
			}
		})
	}
}

func TestCacheControlHeader(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
		MaxAge: 1 * time.Hour,
	})

	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cc := rec.Header().Get("Cache-Control")
	if cc != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=3600")
	}
}

func TestCacheControlNoCache(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
		MaxAge: 0,
	})

	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

func TestLastModifiedHeader(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodGet, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lm := rec.Header().Get("Last-Modified")
	if lm == "" {
		// Embedded files may have zero ModTime, so Last-Modified might be empty.
		// This is acceptable behavior.
		t.Log("Last-Modified header not set (embedded files may have zero ModTime)")
	}
}

func TestFingerprintURL(t *testing.T) {
	tests := []struct {
		path string
		hash string
		want string
	}{
		{"/assets/app.js", "abc123", "/assets/app.abc123.js"},
		{"/style.css", "def456", "/style.def456.css"},
		{"/logo.png", "xyz", "/logo.xyz.png"},
	}

	for _, tt := range tests {
		got := FingerprintURL(tt.path, tt.hash)
		if got != tt.want {
			t.Errorf("FingerprintURL(%q, %q) = %q, want %q", tt.path, tt.hash, got, tt.want)
		}
	}
}

func TestDetectFromName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"file.html", "text/html; charset=utf-8"},
		{"file.css", "text/css; charset=utf-8"},
		{"file.js", "text/javascript; charset=utf-8"},
		{"file.json", "application/json"},
		{"file.png", "image/png"},
		{"file.jpg", "image/jpeg"},
		{"file.gif", "image/gif"},
		{"file.svg", "image/svg+xml"},
		{"file.ico", "image/x-icon"},
		{"file.woff", "font/woff"},
		{"file.woff2", "font/woff2"},
		{"file.ttf", "font/ttf"},
		{"file.eot", "application/vnd.ms-fontobject"},
		{"file.mp4", "video/mp4"},
		{"file.webm", "video/webm"},
		{"file.webp", "image/webp"},
		{"file.pdf", "application/pdf"},
		{"file.unknown", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := DetectFromName(tt.name)
		if got != tt.want {
			t.Errorf("DetectFromName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDetectIgnoresHostMimeDB(t *testing.T) {
	// Linux runners load /etc/mime.types into the stdlib mime package,
	// which answers with non-canonical types for some extensions (CI
	// observed image/vnd.microsoft.icon for .ico and audio/webm for
	// .webm). Simulate that host DB here and assert detection still
	// returns the canonical web type, so serving is identical on every
	// platform.
	for ext, typ := range map[string]string{
		".ico":  "image/vnd.microsoft.icon",
		".webm": "audio/webm",
	} {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			t.Fatalf("AddExtensionType(%q): %v", ext, err)
		}
	}

	if got := DetectFromName("favicon.ico"); got != "image/x-icon" {
		t.Errorf("DetectFromName(favicon.ico) = %q, want image/x-icon", got)
	}
	if got := DetectFromName("clip.webm"); got != "video/webm" {
		t.Errorf("DetectFromName(clip.webm) = %q, want video/webm", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodPost, "/hello.html", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
}

func TestRootPathServesIndex(t *testing.T) {
	handler := Handler(Config{
		FS:     testSubFS(),
		Prefix: "/static",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, expected text/html", ct)
	}
}
