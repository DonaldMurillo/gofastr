package auth

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// RateLimiterConfig is framework/ratelimit's Config.
type RateLimiterConfig = ratelimit.Config

// RateLimiter is framework/ratelimit's limiter answering with auth's 429 shape.
type RateLimiter struct {
	*ratelimit.Limiter
}

// NewRateLimiter returns a RateLimiter for cfg.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{Limiter: ratelimit.NewLimiter(cfg)}
}

func newScopedRateLimiter(cfg RateLimiterConfig, scope string) *RateLimiter {
	if cfg.Scope == "" {
		cfg.Scope = scope
	}
	return NewRateLimiter(cfg)
}

// Middleware limits by client IP, answering 429 with Retry-After.
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

func (rl *RateLimiter) guard(w http.ResponseWriter, r *http.Request) bool {
	allowed, retry := rl.AllowContext(r.Context(), rl.clientIP(r))
	if !allowed {
		w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
		writeAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
		return false
	}
	return true
}

func (rl *RateLimiter) clientIP(r *http.Request) string {
	return rl.Limiter.ClientKey(r)
}
