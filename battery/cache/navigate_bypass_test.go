package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// #669 (was #657): the cache key ignored X-Gofastr-Navigate, so a
// cookieless client-side navigation fetch could be answered with the
// cached FULL page (stored by a plain GET of the same URL), and a
// navigation's partial could be stored under the full page's key and
// replayed to the next plain GET. A navigation is a user action
// expecting current state — the same doctrine that keeps SSE off user
// actions — so navigation fetches bypass the cache entirely: never
// served from it, never stored into it.
func TestNavigationFetchesBypassCache(t *testing.T) {
	c := NewMemoryCache()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("PARTIAL"))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("FULL PAGE"))
	})
	h := CacheMiddleware(c, time.Minute)(handler)

	// A plain cookieless GET populates the cache with the full page.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders", nil))
	if w.Body.String() != "FULL PAGE" || w.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("plain GET: body %q, X-Cache %q", w.Body.String(), w.Header().Get("X-Cache"))
	}

	// The navigation fetch of the same URL must NOT be answered with the
	// cached full page.
	nav := httptest.NewRequest(http.MethodGet, "/orders", nil)
	nav.Header.Set("X-Gofastr-Navigate", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, nav)
	if got := w.Header().Get("X-Cache"); got != "BYPASS" {
		t.Errorf("SECURITY: [navigate-cache] navigation fetch served from the page cache: X-Cache %q", got)
	}
	if w.Body.String() != "PARTIAL" {
		t.Errorf("navigation fetch got %q, want the handler's partial", w.Body.String())
	}

	// And the navigation's partial must not have been stored under the
	// full page's key: the next plain GET still gets the full page.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders", nil))
	if body := w.Body.String(); body != "FULL PAGE" || strings.Contains(body, "PARTIAL") {
		t.Errorf("plain GET after a navigation got %q, want the full page", body)
	}
}
