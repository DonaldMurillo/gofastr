package static

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestNotFoundFileServedWith404(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("home")},
		"404.html":   {Data: []byte("custom miss")},
	}
	h := Handler(Config{FS: fsys, NotFoundFile: "404.html"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "custom miss" {
		t.Fatalf("miss = %d %q, want 404 with the 404.html body", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") != "" {
		t.Fatal("404 page carries an ETag")
	}

	// A cached ETag of the 404 page must not turn a miss into a 304.
	etagReq := httptest.NewRequest(http.MethodGet, "/404.html", nil)
	etagRec := httptest.NewRecorder()
	h.ServeHTTP(etagRec, etagReq)
	cond := httptest.NewRequest(http.MethodGet, "/nope", nil)
	cond.Header.Set("If-None-Match", etagRec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, cond)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("conditional miss = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("HEAD miss = %d, want 404", rec.Code)
	}
}

func TestNotFoundFileMissingFallsBackToPlain404(t *testing.T) {
	h := Handler(Config{FS: fstest.MapFS{"index.html": {Data: []byte("home")}}, NotFoundFile: "404.html"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "404 page not found\n" {
		t.Fatalf("miss = %d %q, want the plain 404", rec.Code, rec.Body.String())
	}
}

func TestNotFoundFileIgnoredInSPAMode(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html": {Data: []byte("app")},
		"404.html":   {Data: []byte("custom miss")},
	}
	h := Handler(Config{FS: fsys, SPA: true, NotFoundFile: "404.html"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/deep/route", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "app" {
		t.Fatalf("SPA miss = %d %q, want the index", rec.Code, rec.Body.String())
	}
}
