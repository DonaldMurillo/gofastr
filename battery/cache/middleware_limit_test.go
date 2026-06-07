package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCacheMiddleware_OverCapStreamsAndDoesNotStore pins the body-size cap: a
// response larger than the limit is delivered in full to the client but never
// cached (the buffer is bounded), while a small response still caches.
func TestCacheMiddleware_OverCapStreamsAndDoesNotStore(t *testing.T) {
	c := NewMemoryCache()
	big := strings.Repeat("x", 4096)

	var hits int
	h := CacheMiddlewareWithLimit(c, time.Minute, 1024)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			hits++
			_, _ = w.Write([]byte(big))
		}))

	// First request: full body delivered, but too big to cache.
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, httptest.NewRequest(http.MethodGet, "/big", nil))
	if rr1.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr1.Code)
	}
	if rr1.Body.Len() != len(big) {
		t.Fatalf("delivered %d bytes, want %d (truncated!)", rr1.Body.Len(), len(big))
	}
	if got := rr1.Header().Get("X-Cache"); got != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", got)
	}

	// Second request must hit the origin again — proof it was NOT cached.
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/big", nil))
	if hits != 2 {
		t.Errorf("origin hits = %d, want 2 (oversize response should not be cached)", hits)
	}
	if rr2.Body.Len() != len(big) {
		t.Errorf("second delivery %d bytes, want %d", rr2.Body.Len(), len(big))
	}
}

// TestCacheMiddleware_UnderCapStillCaches confirms the cap doesn't break normal
// caching: a small response is served from cache on the second request.
func TestCacheMiddleware_UnderCapStillCaches(t *testing.T) {
	c := NewMemoryCache()
	var hits int
	h := CacheMiddlewareWithLimit(c, time.Minute, 1024)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			hits++
			_, _ = w.Write([]byte("small"))
		}))

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/small", nil))
		if rr.Body.String() != "small" {
			t.Fatalf("body = %q", rr.Body.String())
		}
	}
	if hits != 1 {
		t.Errorf("origin hits = %d, want 1 (second served from cache)", hits)
	}
}
