package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiterConfig controls a per-key sliding-window limiter.
//
// MaxAttempts requests are permitted within Window. The MaxAttempts+1th
// request triggers a block of BlockDuration during which every request
// gets 429.
//
// Defaults (filled in by NewRateLimiter when zero): MaxAttempts=10,
// Window=15m, BlockDuration=30m. Same shape as framework/auth.Guard so
// the two implementations stay in sync.
//
// TrustForwardedFor: when true, the leftmost X-Forwarded-For entry is used
// as the client IP. ONLY enable this if the server sits behind a trusted
// reverse proxy that strips client-supplied XFF headers — otherwise an
// attacker rotates the header per request and bypasses every per-IP limit.
// Default is false (use the connection's RemoteAddr).
type RateLimiterConfig struct {
	MaxAttempts       int
	Window            time.Duration
	BlockDuration     time.Duration
	TrustForwardedFor bool
}

// RateLimiter is an in-memory sliding-window rate limiter keyed by an
// arbitrary string (typically the client IP).
type RateLimiter struct {
	cfg    RateLimiterConfig
	mu     sync.Mutex
	states map[string]*rlState
}

type rlState struct {
	attempts     []time.Time
	blockedUntil time.Time
}

// NewRateLimiter constructs a RateLimiter with the given config. Zero
// fields fall back to the documented defaults.
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.Window <= 0 {
		cfg.Window = 15 * time.Minute
	}
	if cfg.BlockDuration <= 0 {
		cfg.BlockDuration = 30 * time.Minute
	}
	return &RateLimiter{cfg: cfg, states: make(map[string]*rlState)}
}

// Allow records an attempt for key and returns whether it is allowed.
// If not allowed, retryAfter is the duration the caller should communicate
// in a Retry-After header.
func (rl *RateLimiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	state, ok := rl.states[key]
	if !ok {
		state = &rlState{}
		rl.states[key] = state
	}

	// Honour an active block.
	if !state.blockedUntil.IsZero() {
		if now.Before(state.blockedUntil) {
			return false, state.blockedUntil.Sub(now)
		}
		// Block has elapsed — clear and continue.
		state.blockedUntil = time.Time{}
		state.attempts = state.attempts[:0]
	}

	// Drop attempts outside the rolling window before counting (same
	// invariant as framework/auth/guard.go after the round-1 fix).
	cutoff := now.Add(-rl.cfg.Window)
	valid := state.attempts[:0]
	for _, t := range state.attempts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	state.attempts = valid

	if len(state.attempts) >= rl.cfg.MaxAttempts {
		state.blockedUntil = now.Add(rl.cfg.BlockDuration)
		return false, rl.cfg.BlockDuration
	}

	state.attempts = append(state.attempts, now)
	return true, 0
}

// Middleware returns an HTTP middleware that rate-limits by client IP.
// Blocked requests get 429 with a Retry-After header.
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

// guard is the shared check used by both Middleware and direct handler
// wrapping. Writes 429 + Retry-After when blocked and returns false.
func (rl *RateLimiter) guard(w http.ResponseWriter, r *http.Request) bool {
	key := rl.clientIP(r)
	allowed, retry := rl.Allow(key)
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
		writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

// clientIP extracts the request IP. Honours X-Forwarded-For only when the
// limiter is explicitly configured with TrustForwardedFor=true (typically
// behind a trusted reverse proxy that strips client-supplied XFF). The
// default ignores XFF — otherwise a single curl with a rotating
// X-Forwarded-For header bypasses every per-IP limit.
func (rl *RateLimiter) clientIP(r *http.Request) string {
	if rl.cfg.TrustForwardedFor {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if comma := strings.IndexByte(xff, ','); comma >= 0 {
				return strings.TrimSpace(xff[:comma])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
