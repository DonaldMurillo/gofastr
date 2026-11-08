package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #669 (was #657): the auth limiters formatted Retry-After with %.0f,
// which rounds to nearest, so a blocked window with under half a second
// left answered "Retry-After: 0" — telling a still-blocked client to
// retry at once. The header must round up: while the caller is limited,
// at least one whole second is the honest answer.
func TestRetryAfterNeverZeroWhileLimited(t *testing.T) {
	rl := newScopedRateLimiter(RateLimiterConfig{
		MaxAttempts:   1,
		Window:        time.Second,
		BlockDuration: 400 * time.Millisecond,
	}, "retry_after_test")
	allowed := rl.guard(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/login", nil))
	if !allowed {
		t.Fatal("first attempt was not allowed")
	}
	blocked := httptest.NewRecorder()
	if rl.guard(blocked, httptest.NewRequest(http.MethodPost, "/login", nil)) {
		t.Fatal("second attempt was allowed inside the window")
	}
	if got := blocked.Header().Get("Retry-After"); got != "1" {
		t.Errorf("SECURITY: [retry-after-zero] Retry-After under a half-second window = %q, want \"1\"", got)
	}
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", blocked.Code)
	}
}
