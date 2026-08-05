// Package ratelimit is the framework's general-purpose HTTP rate limiter.
//
// It enforces a per-key sliding-window policy: up to [Config.MaxAttempts]
// requests are admitted within [Config.Window]; the next request triggers a
// hard block lasting [Config.BlockDuration] during which every request for
// that key gets 429. This is the "N actions per period, then lockout" shape —
// the right tool for brute-force surfaces, write-heavy endpoints, and any
// route where a steady refill rate (the token-bucket model) is the wrong
// abstraction.
//
// # Sliding window vs. token bucket
//
// GoFastr ships two limiters with deliberately different semantics:
//
//   - framework/ratelimit (this package): sliding window + lockout. Use it when
//     you want "at most N per period, then block". The auth battery builds its
//     login / register / password-reset limiters on top of it.
//   - core/middleware.RateLimit: token bucket. Use it for steady API throughput
//     ("1 req/s sustained, burst of 60") and when you want the RateLimit-*
//     budget headers so well-behaved clients can self-pace.
//
// # Per-replica, not distributed
//
// With a nil [Config.Store] the window is held in process memory, so the budget
// is per-replica: N replicas each allow MaxAttempts. For a replica-wide (or
// fleet-wide) budget, supply a [Store] such as battery/auth.SQLRateLimitStore —
// see "Shared store" below.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config controls a per-key sliding-window limiter.
//
// MaxAttempts requests are permitted within Window. The MaxAttempts+1th
// request triggers a block of BlockDuration during which every request for
// that key gets 429.
//
// Defaults (filled in by NewLimiter when zero): MaxAttempts=10, Window=15m,
// BlockDuration=30m.
//
// TrustForwardedFor: when true, the leftmost X-Forwarded-For entry is used as
// the client IP by the default (IP-keyed) middleware. ONLY enable this if the
// server sits behind a trusted reverse proxy that strips client-supplied XFF
// headers — otherwise an attacker rotates the header per request and bypasses
// every per-IP limit. Default is false (use the connection's RemoteAddr).
//
// Store: when non-nil, attempts are recorded in the shared backend instead of
// process memory, so the budget holds across replicas: MaxAttempts total, not
// MaxAttempts × N, and a block on one replica blocks on all. On a store error
// the limiter fails CLOSED (denies) — an attacker must never be able to lift
// the limit by degrading its backend. One store instance can back several
// limiters: keys are namespaced by Scope.
//
// Scope namespaces this limiter's keys inside a shared Store. Set it explicitly
// when several limiters share one Store so their keys never collide. Ignored
// when Store is nil.
type Config struct {
	MaxAttempts       int
	Window            time.Duration
	BlockDuration     time.Duration
	TrustForwardedFor bool
	Store             Store
	Scope             string

	// DevMode relaxes the limiter: when true, AllowContext short-circuits and
	// admits every attempt without touching either backend. Intended ONLY for
	// non-production deploys so local tooling that hammers an endpoint from one
	// IP (localhost) is never locked out. The default (false) keeps the limiter
	// fail-closed — production must NEVER set this.
	DevMode bool
}

// Store is the optional shared backend for a Limiter. When Config.Store is set,
// every replica consults the same attempt ledger, so the budget stays
// MaxAttempts total instead of MaxAttempts × replicas and a block on one
// replica holds on all of them.
//
// battery/auth.SQLRateLimitStore implements this over SQLite or PostgreSQL and
// is the reference implementation; a custom Redis/etcd backend only needs to
// satisfy this interface. The implementation must derive per-limiter state from
// the namespaced key alone — it receives the full Config but should treat Scope
// + key as the identity.
type Store interface {
	Allow(ctx context.Context, key string, cfg Config) (allowed bool, retryAfter time.Duration, err error)
}

// maxKeys caps the number of distinct keys the in-memory limiter tracks at
// once. A per-account limiter keys on attacker-chosen input (lowercased email)
// before the user-store lookup, so an attacker can mint an unbounded number of
// distinct keys. Without a cap the map grows until OOM. When the cap is hit,
// idle states (no active block, no in-window attempts) are reclaimed first; if
// every tracked key is still active the soonest-to-expire are dropped —
// fail-open for that key is acceptable since the alternative is process death,
// and the cap is far above any legitimate concurrent caller count.
const maxKeys = 100_000

// storeErrRetryAfter is the Retry-After hint returned when the shared store
// errors and the limiter fails closed. Short: the outage is likely transient,
// and an auth-backed login path is failing on the same backend anyway.
const storeErrRetryAfter = 30 * time.Second

// Limiter is a sliding-window rate limiter keyed by an arbitrary string
// (typically the client IP). The zero value is not usable — construct one with
// NewLimiter.
type Limiter struct {
	cfg       Config
	mu        sync.Mutex
	states    map[string]*rlState
	lastSweep time.Time
}

type rlState struct {
	attempts     []time.Time
	blockedUntil time.Time
}

// NewLimiter constructs a Limiter with the given config. Zero fields fall back
// to the documented defaults.
func NewLimiter(cfg Config) *Limiter {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.Window <= 0 {
		cfg.Window = 15 * time.Minute
	}
	if cfg.BlockDuration <= 0 {
		cfg.BlockDuration = 30 * time.Minute
	}
	return &Limiter{cfg: cfg, states: make(map[string]*rlState)}
}

// Allow records an attempt for key and returns whether it is allowed. If not
// allowed, retryAfter is the duration the caller should communicate in a
// Retry-After header. Equivalent to AllowContext with a background context —
// HTTP paths should prefer AllowContext(r.Context(), key) so a shared store can
// observe request cancellation.
func (rl *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	return rl.AllowContext(context.Background(), key)
}

// AllowContext records an attempt for key against the configured backend: the
// shared Store when one is set (replica-wide budget), the in-process sliding
// window otherwise. A store failure DENIES the attempt — the limiter guards
// brute-force surfaces, so it must fail closed: degrading its backend must
// never lift the limit.
//
// DevMode (see Config) is an explicit, tested short-circuit: when set, every
// attempt is admitted without touching either backend. This is the dev-only
// relaxation that stops local tooling being locked out; production never sets
// it, so the fail-closed guarantee holds.
func (rl *Limiter) AllowContext(ctx context.Context, key string) (allowed bool, retryAfter time.Duration) {
	if rl.cfg.DevMode {
		return true, 0
	}
	if rl.cfg.Store != nil {
		ok, retry, err := rl.cfg.Store.Allow(ctx, rl.cfg.Scope+"|"+key, rl.cfg)
		if err != nil {
			slog.Default().Warn("ratelimit: shared store error — failing closed",
				"scope", rl.cfg.Scope, "err", err)
			return false, storeErrRetryAfter
		}
		return ok, retry
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Amortized sweep of idle states. Runs at most once per Window so the cost
	// is negligible, and unconditionally when the cap is hit. This keeps the
	// map bounded under an attacker-key flood instead of growing until OOM.
	if len(rl.states) >= maxKeys || now.Sub(rl.lastSweep) >= rl.cfg.Window {
		rl.evictLocked(now)
		rl.lastSweep = now
	}

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

	// Drop attempts outside the rolling window before counting.
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

// evictLocked reclaims map entries that no longer carry security-relevant
// state: those with no active block AND no attempts inside the rolling window.
// Dropping such a state is safe — re-creating it lazily yields the identical
// "fresh key" behaviour. Callers MUST hold rl.mu.
//
// If the map is still at/over the cap after shedding idle entries (i.e. every
// tracked key is actively blocked or mid-window), the entries whose blocks
// expire soonest are dropped to keep the map strictly bounded. An active block
// is preserved as long as the map has room, so eviction is never a routine
// block-bypass — it only sheds the soonest-to-expire blocks under genuine flood
// pressure, which is strictly better than OOM.
func (rl *Limiter) evictLocked(now time.Time) {
	cutoff := now.Add(-rl.cfg.Window)
	for key, st := range rl.states {
		blockActive := !st.blockedUntil.IsZero() && now.Before(st.blockedUntil)
		if blockActive {
			continue
		}
		hasRecent := false
		for _, t := range st.attempts {
			if t.After(cutoff) {
				hasRecent = true
				break
			}
		}
		if !hasRecent {
			delete(rl.states, key)
		}
	}

	if len(rl.states) < maxKeys {
		return
	}

	// Still at/over the cap after shedding idle entries. Shed the entries whose
	// blocks expire soonest (unblocked entries sort first, as their zero
	// blockedUntil is the earliest) down to a low-water mark, so this expensive
	// path runs at most once per ~10% of the cap rather than on every insert
	// under sustained flood.
	lowWater := maxKeys * 9 / 10
	type expiring struct {
		key string
		at  time.Time
	}
	pending := make([]expiring, 0, len(rl.states))
	for key, st := range rl.states {
		pending = append(pending, expiring{key: key, at: st.blockedUntil})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].at.Before(pending[j].at) })
	for i := 0; i < len(pending) && len(rl.states) > lowWater; i++ {
		delete(rl.states, pending[i].key)
	}
}

// Middleware returns an HTTP middleware that rate-limits by client IP (the
// default key). Blocked requests get 429 with a Retry-After header.
//
// It emits ONLY Retry-After and never the RateLimit-Limit /
// RateLimit-Remaining / RateLimit-Reset budget headers that the token-bucket
// middleware (core/middleware.RateLimit) exposes: a live remaining-attempt
// count on a lockout-style limiter would hand an attacker exact brute-force
// pacing information on security-sensitive routes, and adds nothing useful on
// non-security routes where Retry-After already lets a client back off. For
// budget headers and a refill-rate model, use core/middleware.RateLimit.
//
// To group by something other than IP (API key, user id, route param), use
// MiddlewareByKey.
func (rl *Limiter) Middleware() func(http.Handler) http.Handler {
	return rl.MiddlewareByKey(func(r *http.Request) string {
		return ClientIP(r, rl.cfg.TrustForwardedFor)
	})
}

// MiddlewareByKey returns an HTTP middleware that rate-limits using keyFunc to
// derive the per-request identity. A nil keyFunc falls back to the client IP.
// Blocked requests get 429 with a Retry-After header; see Middleware for why the
// budget headers are intentionally omitted.
func (rl *Limiter) MiddlewareByKey(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = func(r *http.Request) string { return ClientIP(r, rl.cfg.TrustForwardedFor) }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retry := rl.AllowContext(r.Context(), keyFunc(r))
			if !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extracts the request IP. It honours X-Forwarded-For only when
// trustXFF is true (typically behind a trusted reverse proxy that strips
// client-supplied XFF). The default (trustXFF=false) ignores XFF — otherwise a
// single curl with a rotating X-Forwarded-For header bypasses every per-IP
// limit.
func ClientIP(r *http.Request, trustXFF bool) string {
	if trustXFF {
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
