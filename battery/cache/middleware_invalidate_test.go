package cache_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/cache"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// invalidateServer serves /items/1 behind CacheMiddleware from body,
// which an unsafe request to any path rewrites.
func invalidateServer(t *testing.T, body *string) http.Handler {
	t.Helper()
	c := cache.NewMemoryCache()
	t.Cleanup(func() { c.Close() })
	return cache.CacheMiddleware(c, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, *body)
			return
		}
		*body = "v2"
		w.WriteHeader(http.StatusNoContent)
	}))
}

func doCache(h http.Handler, method, target string) (string, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Body.String(), rec.Header().Get("X-Cache")
}

// RFC 9111 §4.4: an unsafe request invalidates the stored GET of its
// target URI. After a DELETE the anonymous GET kept answering HIT with
// the old body until the TTL ran out.
func TestUnsafeRequestEvictsGET(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPut, http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			body := "v1"
			h := invalidateServer(t, &body)
			doCache(h, http.MethodGet, "/items/1?x=1")
			if got, xc := doCache(h, http.MethodGet, "/items/1?x=1"); got != "v1" || xc != "HIT" {
				t.Fatalf("second GET = %q %s, want a v1 HIT", got, xc)
			}
			doCache(h, method, "/items/1?x=1")
			if got, xc := doCache(h, http.MethodGet, "/items/1?x=1"); got != "v2" || xc != "MISS" {
				t.Errorf("GET after %s = %q %s, want a v2 MISS", method, got, xc)
			}
		})
	}
}

// The router strips a locale prefix before the middleware runs, so each
// locale's page is its own entry. A DELETE through /es/items/1 changes
// the record on every locale's page, the default locale's included.
func TestUnsafeRequestEvictsEveryLocale(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}}
	body := "v1"
	h := invalidateServer(t, &body)
	do := func(method, tag string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/items/1", nil)
		h.ServeHTTP(rec, req.WithContext(i18n.WithPathLocale(req.Context(), set, tag)))
		return rec.Header().Get("X-Cache")
	}
	for _, tag := range []string{"en", "es", "fr"} {
		do(http.MethodGet, tag)
		if xc := do(http.MethodGet, tag); xc != "HIT" {
			t.Fatalf("%s GET = %s, want HIT", tag, xc)
		}
	}
	do(http.MethodDelete, "es")
	for _, tag := range []string{"en", "es", "fr"} {
		if xc := do(http.MethodGet, tag); xc != "MISS" {
			t.Errorf("%s GET after the es DELETE = %s, want MISS", tag, xc)
		}
	}
}

// Safe methods change nothing, so they leave the stored GET alone, and
// an unsafe request evicts only its own target.
func TestSafeRequestKeepsGET(t *testing.T) {
	body := "v1"
	h := invalidateServer(t, &body)
	doCache(h, http.MethodGet, "/items/1")
	for _, method := range []string{http.MethodHead, http.MethodOptions, http.MethodTrace} {
		doCache(h, method, "/items/1")
		body = "v1"
		if _, xc := doCache(h, http.MethodGet, "/items/1"); xc != "HIT" {
			t.Errorf("GET after %s = %s, want HIT", method, xc)
		}
	}
	doCache(h, http.MethodDelete, "/items/2")
	body = "v1"
	if _, xc := doCache(h, http.MethodGet, "/items/1"); xc != "HIT" {
		t.Errorf("a DELETE of /items/2 evicted /items/1")
	}
}
