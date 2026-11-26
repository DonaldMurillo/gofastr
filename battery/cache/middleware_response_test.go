package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConditionalGETReachesOrigin(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	h := CacheMiddleware(c, 0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte("document"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", `"v1"`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("got %d %q", w.Code, w.Body)
	}
}

func TestFailedHijackKeepsResponseWritable(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	h := CacheMiddleware(c, 0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, _, err := http.NewResponseController(w).Hijack(); err == nil {
			t.Fatal("unexpected hijack")
		}
		http.Error(w, "upgrade unavailable", http.StatusNotImplemented)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusNotImplemented || !strings.Contains(w.Body.String(), "upgrade unavailable") {
		t.Fatalf("got %d %q", w.Code, w.Body)
	}
}

type hintsRecorder struct {
	*httptest.ResponseRecorder
	statuses []int
}

func (w *hintsRecorder) WriteHeader(code int) {
	w.statuses = append(w.statuses, code)
	if code >= 200 {
		w.ResponseRecorder.WriteHeader(code)
	}
}

func TestCacheMissForwardsEarlyHints(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	h := CacheMiddleware(c, 0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("Link")
		w.WriteHeader(http.StatusOK)
	}))
	w := &hintsRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if len(w.statuses) != 2 || w.statuses[0] != http.StatusEarlyHints || w.statuses[1] != http.StatusOK {
		t.Fatalf("statuses = %v", w.statuses)
	}
}

func TestCacheStreamPreservesTrailersAfterFlush(t *testing.T) {
	c := NewMemoryCache()
	defer c.Close()
	h := CacheMiddleware(c, 0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Completion")
		_, _ = w.Write([]byte("body"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("X-Completion", "ok")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	got := rec.Result()
	defer got.Body.Close()
	if got.Trailer.Get("X-Completion") != "ok" {
		t.Fatalf("trailers = %v", got.Trailer)
	}
}
