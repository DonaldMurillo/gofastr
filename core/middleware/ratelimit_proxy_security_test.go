package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newProxyAwareRateLimitedServer(capacity int, handler http.HandlerFunc) (http.Handler, *int32) {
	var allowed int32
	cfg := RateLimitConfig{
		Capacity:          capacity,
		RefillEvery:       time.Minute,
		RefillBy:          1,
		TrustProxyHeaders: true,
	}
	srv := RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&allowed, 1)
		handler(w, r)
	}))
	return srv, &allowed
}

// Property: with TrustProxyHeaders, only a well-formed public IP in a
// trusted proxy header may re-key the bucket. Every spoofable shape —
// rotating public values (a fresh "client" per request), private ranges,
// and arbitrary non-IP strings — must fall back to the connection
// address, so all five requests share the one bucket of capacity 2.
func TestRateLimit_TrustProxyHeadersSpoofedValuesShareOneBucket(t *testing.T) {
	spoofs := []struct {
		name   string
		header string
		value  func(i int) string
		reason string
	}{
		{
			name:   "XFF rotation",
			header: "X-Forwarded-For",
			value:  func(i int) string { return fmt.Sprintf("198.51.100.%d", i) },
			reason: "rotating X-Forwarded-For bypassed proxy-aware rate limit",
		},
		{
			name:   "XFF private",
			header: "X-Forwarded-For",
			value:  func(i int) string { return fmt.Sprintf("10.0.0.%d", i) },
			reason: "private X-Forwarded-For values were trusted and bypassed rate limit",
		},
		{
			name:   "XFF arbitrary",
			header: "X-Forwarded-For",
			value:  func(i int) string { return fmt.Sprintf("attacker-%d", i) },
			reason: "arbitrary X-Forwarded-For strings were trusted and bypassed rate limit",
		},
		{
			name:   "X-Real-IP rotation",
			header: "X-Real-IP",
			value:  func(i int) string { return fmt.Sprintf("198.51.100.%d", i) },
			reason: "rotating X-Real-IP bypassed proxy-aware rate limit",
		},
	}
	for _, spoof := range spoofs {
		t.Run(spoof.name, func(t *testing.T) {
			srv, allowed := newProxyAwareRateLimitedServer(2, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			for i := range 5 {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = "203.0.113.10:1234"
				req.Header.Set(spoof.header, spoof.value(i))
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
			}
			if got := atomic.LoadInt32(allowed); got > 2 {
				t.Fatalf("SECURITY: [ratelimit-proxy] %s: allowed=%d cap=2.", spoof.reason, got)
			}
		})
	}
}
