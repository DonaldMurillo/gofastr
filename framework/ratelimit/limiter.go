// Package ratelimit is a per-key sliding-window rate limiter with lockout: [NewLimiter], [Limiter.AllowContext], [Limiter.Middleware].
package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// NamespaceScope escapes "%" and "|" in a Store scope so the scope/key boundary stays unambiguous.
func NamespaceScope(scope string) string {
	return strings.NewReplacer("%", "%25", "|", "%7C").Replace(scope)
}

// Config admits MaxAttempts requests per key within Window, then refuses the key for BlockDuration
// (defaults 10, 15m, 30m). Store shares it across replicas under Scope; TrustForwardedFor keys on X-Forwarded-For from TrustedProxies.
type Config struct {
	MaxAttempts       int
	Window            time.Duration
	BlockDuration     time.Duration
	TrustForwardedFor bool
	TrustedProxies    []string
	Store             Store
	Scope             string

	// DevMode admits every attempt without touching either backend; never set it in production.
	DevMode bool
}

// Store is a shared attempt ledger for a Limiter (battery/auth.SQLRateLimitStore is one); Allow must
// decide atomically per namespaced key so a concurrent burst admits at most MaxAttempts.
type Store interface {
	Allow(ctx context.Context, key string, cfg Config) (allowed bool, retryAfter time.Duration, err error)
}

const maxKeys = 100_000

const storeErrRetryAfter = 30 * time.Second

// Limiter is a sliding-window rate limiter keyed by an arbitrary string; construct one with NewLimiter.
type Limiter struct {
	cfg       Config
	proxies   clientip.Proxies
	mu        sync.Mutex
	states    map[string]*rlState
	lastSweep time.Time
	blockSeq  uint64
}

type rlState struct {
	attempts     []time.Time
	blockedUntil time.Time
	blockOrder   uint64
}

func (s *rlState) expireBlock(now time.Time) {
	if !s.blockedUntil.IsZero() && !now.Before(s.blockedUntil) {
		s.blockedUntil = time.Time{}
		s.attempts = s.attempts[:0]
		s.blockOrder = 0
	}
}

// NewLimiter constructs a Limiter, filling zero Config fields with their defaults.
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
	return &Limiter{cfg: cfg, proxies: clientip.ParseProxies(cfg.TrustedProxies), states: make(map[string]*rlState)}
}

// Allow is AllowContext with a background context.
func (rl *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	return rl.AllowContext(context.Background(), key)
}

const maxKeyLen = 256

func foldKey(key string) string {
	if len(key) <= maxKeyLen && textsafe.Storable(key) && !strings.HasPrefix(key, "sha256:") {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AllowContext records an attempt for key and reports whether it is allowed and, if not, how long to wait.
func (rl *Limiter) AllowContext(ctx context.Context, key string) (allowed bool, retryAfter time.Duration) {
	allowed, retryAfter, err := rl.Admit(ctx, key)
	if err != nil {
		slog.Default().Warn("ratelimit: shared store error: failing closed",
			"scope", rl.cfg.Scope, "err", err)
	}
	return allowed, retryAfter
}

// Admit is AllowContext that also returns the shared store's error; a store error still denies.
func (rl *Limiter) Admit(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error) {
	if rl.cfg.DevMode {
		return true, 0, nil
	}
	key = foldKey(key)
	if rl.cfg.Store != nil {
		ok, retry, err := rl.cfg.Store.Allow(ctx, NamespaceScope(rl.cfg.Scope)+"|"+key, rl.cfg)
		if err != nil {
			return false, storeErrRetryAfter, err
		}
		return ok, retry, nil
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	if len(rl.states) >= maxKeys || now.Sub(rl.lastSweep) >= rl.cfg.Window {
		rl.evictLocked(now)
		rl.lastSweep = now
	}

	state, ok := rl.states[key]
	if !ok {
		state = &rlState{}
		rl.states[key] = state
	}

	state.expireBlock(now)
	if !state.blockedUntil.IsZero() {
		return false, state.blockedUntil.Sub(now), nil
	}

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
		rl.blockSeq++
		state.blockOrder = rl.blockSeq
		return false, rl.cfg.BlockDuration, nil
	}

	state.attempts = append(state.attempts, now)
	return true, 0, nil
}

func (rl *Limiter) evictLocked(now time.Time) {
	cutoff := now.Add(-rl.cfg.Window)
	for key, st := range rl.states {
		st.expireBlock(now)
		if !st.blockedUntil.IsZero() {
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

	lowWater := maxKeys * 9 / 10
	type expiring struct {
		key   string
		at    time.Time
		order uint64
	}
	pending := make([]expiring, 0, len(rl.states))
	for key, st := range rl.states {
		pending = append(pending, expiring{key: key, at: st.blockedUntil, order: st.blockOrder})
	}
	sort.Slice(pending, func(i, j int) bool {
		iBlocked, jBlocked := !pending[i].at.IsZero(), !pending[j].at.IsZero()
		if iBlocked != jBlocked {
			return !iBlocked
		}
		if !iBlocked {
			return false
		}
		if !pending[i].at.Equal(pending[j].at) {
			return pending[i].at.After(pending[j].at)
		}
		return pending[i].order > pending[j].order
	})
	for i := 0; i < len(pending) && len(rl.states) > lowWater; i++ {
		delete(rl.states, pending[i].key)
	}
}

// Middleware rate-limits by ClientKey, answering a blocked request 429 with Retry-After.
func (rl *Limiter) Middleware() func(http.Handler) http.Handler {
	return rl.MiddlewareByKey(rl.ClientKey)
}

// MiddlewareByKey is Middleware keyed by keyFunc; a nil keyFunc means ClientKey.
func (rl *Limiter) MiddlewareByKey(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	if keyFunc == nil {
		keyFunc = rl.ClientKey
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, retry := rl.AllowContext(r.Context(), keyFunc(r))
			if !allowed {
				w.Header().Set("Retry-After", RetryAfterSeconds(retry))
				i18n.HTTPError(w, r, http.StatusTooManyRequests, reasonRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RetryAfterSeconds formats a wait as whole seconds rounded up, "0" for a non-positive one.
func RetryAfterSeconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	secs := d / time.Second
	if d%time.Second > 0 {
		secs++
	}
	return strconv.FormatInt(int64(secs), 10)
}

// ClientIP returns the request's peer address, or with trustXFF the rightmost X-Forwarded-For entry.
func ClientIP(r *http.Request, trustXFF bool) string {
	return clientIPFor(r, trustXFF, clientip.Proxies{})
}

// ClientIP is the package ClientIP under this limiter's TrustForwardedFor and TrustedProxies.
func (rl *Limiter) ClientIP(r *http.Request) string {
	return clientIPFor(r, rl.cfg.TrustForwardedFor, rl.proxies)
}

// ClientKey is the per-client rate-limit key: IPKey of ClientIP.
func (rl *Limiter) ClientKey(r *http.Request) string {
	return IPKey(rl.ClientIP(r))
}

// IPKey is a client address as a rate-limit key: IPv4 as is, IPv6 as its /64, anything else unchanged.
func IPKey(addr string) string {
	return clientip.RateKey(addr)
}

func clientIPFor(r *http.Request, trustXFF bool, proxies clientip.Proxies) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peer = host
	}
	if !trustXFF {
		return peer
	}
	if !proxies.Empty() && !proxies.ContainsAddr(peer) {
		return peer
	}
	if ip, ok := clientip.Forwarded(r.Header, proxies); ok {
		return ip.String()
	}
	return peer
}
