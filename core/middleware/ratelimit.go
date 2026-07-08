package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig controls the in-memory token-bucket rate limiter.
//
// KeyFunc selects the per-bucket identity (per IP, per session, per API
// key, etc.). When KeyFunc is nil the default extractor uses
// r.RemoteAddr — X-Forwarded-For is *ignored* unless TrustProxyHeaders
// is set, because a caller in front of the origin can spoof XFF freely
// and would otherwise get a fresh bucket per request.
//
// Capacity is the maximum number of tokens a bucket can hold (= peak burst
// allowed). RefillEvery / RefillBy together define the steady-state rate:
// "RefillBy tokens are added every RefillEvery". Defaults: 60 tokens, +60
// every minute (i.e., 1 req/sec sustained with a 60-req burst).
//
// When a request is rate-limited the handler responds 429 with a
// Retry-After header indicating seconds until the bucket would have one
// free token.
type RateLimitConfig struct {
	KeyFunc      func(*http.Request) string
	Capacity     int
	RefillEvery  time.Duration
	RefillBy     int
	StatusCode   int
	ErrorMessage string

	// TrustProxyHeaders enables reading the client IP from the
	// leftmost X-Forwarded-For (or X-Real-IP) entry. Only set this
	// when the origin is behind a reverse proxy you control that
	// rewrites or appends the header — otherwise an attacker can
	// trivially defeat per-IP limiting by sending random XFF values.
	//
	// SECURITY: TrustProxyHeaders alone is NOT sufficient. The
	// middleware will only trust the header when r.RemoteAddr (the
	// immediate TCP peer) is one of TrustedProxies — see below.
	// Without a trusted-proxy whitelist, XFF/X-Real-IP are ignored
	// and the key falls back to r.RemoteAddr, so an attacker sending
	// rotating header values from the same source can't get fresh
	// buckets per request.
	TrustProxyHeaders bool

	// TrustedProxies is the set of TCP peer addresses (r.RemoteAddr,
	// port stripped) whose X-Forwarded-For / X-Real-IP headers are
	// trusted when TrustProxyHeaders is true. CIDR notation is
	// accepted; bare IPs are matched exactly. If empty, no proxy is
	// trusted and the header values are ignored (the key falls back
	// to r.RemoteAddr).
	TrustedProxies []string
	// OmitBudgetHeaders, when true, suppresses the IETF-draft
	// RateLimit-Limit / RateLimit-Remaining / RateLimit-Reset headers
	// that the middleware otherwise emits on every response (both
	// allowed and 429). Set this when the per-response header cost
	// matters at very high request rates, or when an upstream cache
	// keys on these headers and would shard its cache by remaining
	// budget. Retry-After on the 429 path is unaffected. Default is
	// false (headers on) so well-behaved API clients can self-pace.
	OmitBudgetHeaders bool
}

// RateLimit returns Middleware that enforces a token-bucket rate limit per
// extracted key. Each key gets its own bucket; expired buckets eventually
// get reaped (every 5 minutes of inactivity).
//
// Usage:
//
//	r.Use(middleware.RateLimit(middleware.RateLimitConfig{
//	    Capacity:    30,
//	    RefillEvery: time.Second,
//	    RefillBy:    1,
//	}))
//
// or via the default:
//
//	r.Use(middleware.RateLimit(middleware.RateLimitConfig{}))   // 60/min/IP
func RateLimit(cfg RateLimitConfig) Middleware {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 60
	}
	if cfg.RefillEvery <= 0 {
		cfg.RefillEvery = time.Minute
	}
	if cfg.RefillBy <= 0 {
		cfg.RefillBy = cfg.Capacity
	}
	if cfg.KeyFunc == nil {
		if cfg.TrustProxyHeaders {
			trusted := parseTrustedProxies(cfg.TrustedProxies)
			cfg.KeyFunc = newProxyAwareRateLimitKey(trusted)
		} else {
			cfg.KeyFunc = defaultRateLimitKey
		}
	}
	if cfg.StatusCode == 0 {
		cfg.StatusCode = http.StatusTooManyRequests
	}
	if cfg.ErrorMessage == "" {
		cfg.ErrorMessage = "rate limit exceeded"
	}

	buckets := newBucketStore(cfg.Capacity, cfg.RefillEvery, cfg.RefillBy)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := cfg.KeyFunc(r)
			allowed, retryAfter, remaining, resetAfter := buckets.take(key)
			if !cfg.OmitBudgetHeaders {
				w.Header().Set("RateLimit-Limit", strconv.Itoa(cfg.Capacity))
				w.Header().Set("RateLimit-Remaining", strconv.Itoa(remaining))
				w.Header().Set("RateLimit-Reset", strconv.Itoa(ceilSeconds(resetAfter)))
			}
			if !allowed {
				if retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				}
				http.Error(w, cfg.ErrorMessage, cfg.StatusCode)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bucketStore is a tiny per-key token-bucket pool with passive refill.
// Buckets are kept in memory; nothing here is persistent. Suitable for
// single-instance deployments and tests; multi-instance setups should swap
// for a Redis-backed implementation behind the same KeyFunc.
type bucketStore struct {
	capacity int
	rate     time.Duration
	refill   int

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens   int
	lastSeen time.Time
}

func newBucketStore(capacity int, rate time.Duration, refill int) *bucketStore {
	return &bucketStore{
		capacity: capacity,
		rate:     rate,
		refill:   refill,
		buckets:  map[string]*bucket{},
	}
}

// take attempts to consume one token from the bucket for key. Returns
// (allowed, retryAfter, remaining, resetAfter), all computed under the store
// lock so a request never takes it twice. retryAfter is the duration until the
// bucket would have at least one token (0 on success); remaining is the token
// count AFTER this request consumed its token (0 on the deny path); resetAfter
// is the duration until the bucket is back at full capacity (0 when already
// full).
func (s *bucketStore) take(key string) (bool, time.Duration, int, time.Duration) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok {
		// Fresh bucket starts full; this request consumes its token.
		remaining := s.capacity - 1
		s.buckets[key] = &bucket{tokens: remaining, lastSeen: now}
		// Opportunistic reap of stale buckets so the map doesn't grow.
		if len(s.buckets)%64 == 0 {
			s.reapLocked(now)
		}
		return true, 0, remaining, s.timeToFull(remaining, now, now)
	}

	// Refill — add floor(elapsed / rate) * refill tokens, capped at capacity.
	if elapsed := now.Sub(b.lastSeen); elapsed > 0 {
		gained := int(elapsed/s.rate) * s.refill
		if gained > 0 {
			b.tokens += gained
			if b.tokens > s.capacity {
				b.tokens = s.capacity
			}
			b.lastSeen = b.lastSeen.Add(time.Duration(gained/s.refill) * s.rate)
		}
	}

	if b.tokens <= 0 {
		// No token available. How long until the next one? rate / refill per
		// token, minus the time already elapsed into the current tick, rounded
		// up to at least a second.
		perToken := s.rate / time.Duration(s.refill)
		retry := perToken - now.Sub(b.lastSeen)
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry, 0, s.timeToFull(0, b.lastSeen, now)
	}
	b.tokens--
	return true, 0, b.tokens, s.timeToFull(b.tokens, b.lastSeen, now)
}

// timeToFull returns the duration until the bucket is back at full capacity,
// given its current token count and the lastSeen timestamp (the moment the
// last consumed refill tick landed). Returns 0 when the bucket is already at
// capacity.
//
// The refill model adds s.refill tokens every s.rate, with the next tick at
// lastSeen+s.rate. Recovering a deficit of D tokens therefore takes
// ceil(D/refill) ticks, landing at lastSeen.Add(ceilTicks*rate); the time
// remaining is that moment minus now, clamped at 0. The result is thus
// monotonically non-decreasing in the deficit (more missing tokens ⇒ a longer
// or equal reset) and consistent with the ticks take actually consumes during
// refill.
func (s *bucketStore) timeToFull(remaining int, lastSeen, now time.Time) time.Duration {
	deficit := s.capacity - remaining
	if deficit <= 0 {
		return 0
	}
	ticks := (deficit + s.refill - 1) / s.refill // ceil(deficit / refill)
	resetAt := lastSeen.Add(time.Duration(ticks) * s.rate)
	if d := resetAt.Sub(now); d > 0 {
		return d
	}
	return 0
}

// ceilSeconds renders d as whole seconds rounded up, with a floor of 0. Used
// for RateLimit-Reset so a client never under-waits the reset window.
func ceilSeconds(d time.Duration) int {
	secs := int64(d / time.Second)
	if d%time.Second > 0 {
		secs++
	}
	if secs < 0 {
		secs = 0
	}
	return int(secs)
}

// reapLocked drops buckets idle for more than 5 minutes. Caller must hold mu.
func (s *bucketStore) reapLocked(now time.Time) {
	cutoff := now.Add(-5 * time.Minute)
	for k, b := range s.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(s.buckets, k)
		}
	}
}

// defaultRateLimitKey extracts a per-client identity from the request
// using r.RemoteAddr only (port stripped). X-Forwarded-For / X-Real-IP
// are deliberately ignored — a client talking directly to the origin
// can put any value in those headers and would otherwise get a fresh
// bucket per request, defeating per-IP rate limiting entirely.
//
// Set TrustProxyHeaders=true on RateLimitConfig (paired with a
// TrustedProxies whitelist) only when the origin sits behind a
// reverse proxy you control.
func defaultRateLimitKey(r *http.Request) string {
	return stripPort(r.RemoteAddr)
}

// newProxyAwareRateLimitKey returns a KeyFunc that trusts the leftmost
// X-Forwarded-For (then X-Real-IP) entry ONLY when r.RemoteAddr matches
// one of the configured trusted proxies. The trusted value must also
// parse as a well-formed public IP — private / loopback / link-local
// ranges and arbitrary strings are rejected so an attacker sending
// junk from a trusted hop can't create fresh buckets per request.
//
// When the header is not trusted (no proxy match, no value, or value
// fails validation), the key falls back to r.RemoteAddr so rotating
// header values from the same TCP source can't bypass the limit.
func newProxyAwareRateLimitKey(trusted []trustedProxy) func(*http.Request) string {
	return func(r *http.Request) string {
		peer := stripPort(r.RemoteAddr)
		if !proxyAllowed(peer, trusted) {
			return peer
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			candidate := xff
			for i := 0; i < len(xff); i++ {
				if xff[i] == ',' {
					candidate = xff[:i]
					break
				}
			}
			candidate = trimSpaces(candidate)
			if isTrustablePublicIP(candidate) {
				return candidate
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			xri = trimSpaces(xri)
			if isTrustablePublicIP(xri) {
				return xri
			}
		}
		return peer
	}
}

// trustedProxy is either a single IP or a CIDR range. Membership is
// checked with contains.
type trustedProxy struct {
	ip  net.IP
	net *net.IPNet
}

func parseTrustedProxies(entries []string) []trustedProxy {
	out := make([]trustedProxy, 0, len(entries))
	for _, e := range entries {
		if e == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(e); err == nil {
			out = append(out, trustedProxy{net: ipnet})
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			out = append(out, trustedProxy{ip: ip})
		}
	}
	return out
}

func proxyAllowed(peer string, trusted []trustedProxy) bool {
	if len(trusted) == 0 {
		return false
	}
	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}
	for _, t := range trusted {
		if t.net != nil {
			if t.net.Contains(ip) {
				return true
			}
			continue
		}
		if t.ip != nil && t.ip.Equal(ip) {
			return true
		}
	}
	return false
}

// isTrustablePublicIP returns true iff s parses as an IP that is
// neither loopback, link-local, nor in a private RFC1918 / ULA range.
// Junk strings and private ranges both return false.
func isTrustablePublicIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return true
}

func trimSpaces(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// stripPort returns the host portion of addr in the canonical form used
// as a rate-limit / proxy-trust key. Both bracketed IPv6 ("[::1]:8080")
// and bare-IPv6 forms ("::1", "2001:db8::1") must round-trip cleanly —
// a last-colon split mangles "2001:db8::1" to "2001:db8:" and silently
// shards the rate-limit bucket per-address.
func stripPort(addr string) string {
	if addr == "" {
		return addr
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	// SplitHostPort rejects unbracketed IPv6 (too many colons) and bare
	// hosts (no colon). Distinguish: bare-IPv6 has >=2 colons, no port.
	if strings.Count(addr, ":") >= 2 {
		return addr
	}
	return addr
}
