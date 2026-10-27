package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// countingCache wraps h in CacheMiddleware over a fresh store, with outer
// running before the cache, and reports how many times h ran.
func countingCache(outer func(http.ResponseWriter), h http.HandlerFunc) (http.Handler, *int) {
	runs := new(int)
	inner := CacheMiddleware(NewMemoryCache(WithTTL(time.Minute)), time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*runs++
		h(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if outer != nil {
			outer(w)
		}
		inner.ServeHTTP(w, r)
	}), runs
}

func serveTwice(t *testing.T, h http.Handler, req func() *http.Request) string {
	t.Helper()
	var last string
	for range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req())
		last = rec.Header().Get("X-Cache")
	}
	return last
}

func plainGet() *http.Request { return httptest.NewRequest(http.MethodGet, "/page", nil) }

func okBody(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("per-user")) }

// Middleware outside the cache that already marked the response private
// was ignored: the storage decision read only the handler's headers, so
// the response was stored and replayed to the next caller.
func TestCacheHonoursOuterNoStore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		outer func(http.ResponseWriter)
	}{
		{"no-store", func(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }},
		{"private", func(w http.ResponseWriter) { w.Header().Set("Cache-Control", "private") }},
		{"set-cookie", func(w http.ResponseWriter) { w.Header().Add("Set-Cookie", "sid=abc") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, runs := countingCache(tc.outer, okBody)
			if got := serveTwice(t, h, plainGet); got != "MISS" || *runs != 2 {
				t.Fatalf("second request X-Cache = %q, handler ran %d time(s); want MISS and 2", got, *runs)
			}
		})
	}
}

// Header.Get reads only the first Cache-Control field line, so a
// no-store on a second line was missed.
func TestCacheReadsEveryCacheControlLine(t *testing.T) {
	t.Run("response", func(t *testing.T) {
		h, runs := countingCache(nil, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Cache-Control", "max-age=60")
			w.Header().Add("Cache-Control", "no-store")
			okBody(w, r)
		})
		if got := serveTwice(t, h, plainGet); got != "MISS" || *runs != 2 {
			t.Fatalf("second request X-Cache = %q, handler ran %d time(s); want MISS and 2", got, *runs)
		}
	})
	for _, directive := range []string{"no-cache", "no-store"} {
		t.Run("request "+directive, func(t *testing.T) {
			h, runs := countingCache(nil, okBody)
			// Prime the entry with a plain request.
			h.ServeHTTP(httptest.NewRecorder(), plainGet())
			req := plainGet()
			req.Header.Add("Cache-Control", "max-age=0")
			req.Header.Add("Cache-Control", directive)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if got := rec.Header().Get("X-Cache"); got != "MISS" || *runs != 2 {
				t.Fatalf("X-Cache = %q, handler ran %d time(s); want the %s request to bypass the stored entry", got, *runs, directive)
			}
		})
	}
}
