package auth

import (
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// RateLimiterConfig is an alias for the general-purpose sliding-window limiter
// config now living in framework/ratelimit. Existing auth callers keep
// compiling; new code should use ratelimit.Config directly. See
// framework/ratelimit (and the "rate-limit" doc topic) for field semantics.
type RateLimiterConfig = ratelimit.Config

// RateLimiter wraps the general-purpose sliding-window limiter from
// framework/ratelimit. It exists to preserve auth's own 429 response shape
// (JSON envelope for API clients, 303 redirect for browser form posts) via the
// package-local guard, the general middleware emits a plain text 429. For
// non-auth routes prefer ratelimit.Limiter + ratelimit.Middleware directly.
//
// Allow / AllowContext are promoted from the embedded limiter, so direct
// callers (e.g. guardAuthLimit, the per-account login limiter) are unchanged.
type RateLimiter struct {
	*ratelimit.Limiter
	trustXFF bool
}

// NewRateLimiter constructs a RateLimiter with the given config. Zero fields
// fall back to the documented defaults (delegated to ratelimit.NewLimiter).
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		Limiter:  ratelimit.NewLimiter(cfg),
		trustXFF: cfg.TrustForwardedFor,
	}
}

// newScopedRateLimiter is NewRateLimiter with a default Scope, used by the
// built-in auth consumers so limiters sharing one RateLimitStore never collide
// on raw IP/email keys. An explicit cfg.Scope wins.
func newScopedRateLimiter(cfg RateLimiterConfig, scope string) *RateLimiter {
	if cfg.Scope == "" {
		cfg.Scope = scope
	}
	return NewRateLimiter(cfg)
}

// Middleware returns an HTTP middleware that rate-limits by client IP using
// auth's 429 response shape (JSON envelope). Blocked requests get 429 with a
// Retry-After header.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.guard(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// guard is the shared check used by Middleware and direct handler wrapping
// (magic-link send, password reset, 2FA challenge, email verification). It
// records an attempt for the client IP and, when blocked, writes 429 +
// Retry-After and returns false. The body uses writeAuthError (the canonical
// JSON envelope), NOT the form-redirect shape, which guardAuthLimit applies
// selectively for browser posts.
func (rl *RateLimiter) guard(w http.ResponseWriter, r *http.Request) bool {
	allowed, retry := rl.AllowContext(r.Context(), rl.clientIP(r))
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
		writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

// clientIP extracts the request IP, honouring X-Forwarded-For only when the
// limiter was configured with TrustForwardedFor=true. Delegates to
// ratelimit.ClientIP so the proxy-trust rule has exactly one implementation.
func (rl *RateLimiter) clientIP(r *http.Request) string {
	return ratelimit.ClientIP(r, rl.trustXFF)
}
