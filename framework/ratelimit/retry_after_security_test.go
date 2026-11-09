package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #669 (was #657): the generic limiter middleware formatted Retry-After
// with %.0f, which rounds to nearest, so a block with under half a
// second left answered "Retry-After: 0" — telling a still-blocked
// client to retry at once. The header must round up: while the caller
// is limited, at least one whole second is the honest answer.
func TestMiddlewareRetryAfterNeverZeroWhileLimited(t *testing.T) {
	rl := NewLimiter(Config{
		MaxAttempts:   1,
		Window:        time.Second,
		BlockDuration: 400 * time.Millisecond,
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := rl.Middleware()(next)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/x", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first attempt not allowed: %d", first.Code)
	}
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/x", nil))
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", blocked.Code)
	}
	if got := blocked.Header().Get("Retry-After"); got != "1" {
		t.Errorf("SECURITY: [retry-after-zero] Retry-After under a half-second block = %q, want \"1\"", got)
	}
	// A huge configured block must not overflow the ceiling into a
	// negative wait (review finding: d+time.Second-1 overflows near
	// MaxInt64 and printed a negative Retry-After).
	if got := RetryAfterSeconds(time.Duration(1)<<62 + 750*time.Millisecond); got != "4611686020" {
		t.Errorf("RetryAfterSeconds near the duration ceiling = %q", got)
	}
}
