package middleware

import (
	"cmp"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// RateLimitConfig controls the in-memory token-bucket rate limiter: Capacity tokens per key (default 60), refilled RefillBy every RefillEvery (default all of them every minute); a refused request gets StatusCode (default 429) and a Retry-After.
type RateLimitConfig struct {
	KeyFunc     func(*http.Request) string
	Capacity    int
	RefillEvery time.Duration
	RefillBy    int
	StatusCode  int
	// Message is the refusal body; zero means "rate limit exceeded" (error.rate_limited).
	Message i18n.Phrase

	// TrustProxyHeaders keys on X-Forwarded-For (read from the right) or X-Real-IP, only when the TCP peer is in TrustedProxies.
	TrustProxyHeaders bool

	// TrustedProxies lists the peer IPs or CIDRs whose forwarding headers are trusted; empty trusts none.
	TrustedProxies []string
	// OmitBudgetHeaders drops the RateLimit-Limit, -Remaining and -Reset headers; Retry-After on a refusal stays.
	OmitBudgetHeaders bool

	// MaxKeys caps the buckets held at once, shedding the fullest first; zero means 100_000.
	MaxKeys int
}

// RateLimit returns middleware that enforces a per-key token bucket, keyed by default on the peer address with an IPv6 peer collapsed to its /64.
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
			cfg.KeyFunc = newProxyAwareRateLimitKey(clientip.ParseProxies(cfg.TrustedProxies))
		} else {
			cfg.KeyFunc = defaultRateLimitKey
		}
	}
	if cfg.StatusCode == 0 {
		cfg.StatusCode = http.StatusTooManyRequests
	}
	if cfg.Message.IsZero() {
		cfg.Message = reasonRateLimited
	}

	buckets := newBucketStoreCapped(cfg.Capacity, cfg.RefillEvery, cfg.RefillBy, cfg.MaxKeys)

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
					w.Header().Set("Retry-After", strconv.Itoa(ceilSeconds(retryAfter)))
				}
				i18n.HTTPError(w, r, cfg.StatusCode, cfg.Message)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type bucketStore struct {
	capacity int
	rate     time.Duration
	refill   int
	maxKeys  int

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens   int
	lastSeen time.Time
}

const defaultRateLimitMaxKeys = 100_000

func newBucketStore(capacity int, rate time.Duration, refill int) *bucketStore {
	return newBucketStoreCapped(capacity, rate, refill, 0)
}

func newBucketStoreCapped(capacity int, rate time.Duration, refill int, maxKeys int) *bucketStore {
	if maxKeys <= 0 {
		maxKeys = defaultRateLimitMaxKeys
	}
	return &bucketStore{
		capacity: capacity,
		rate:     rate,
		refill:   refill,
		maxKeys:  maxKeys,
		buckets:  map[string]*bucket{},
	}
}

func (s *bucketStore) take(key string) (bool, time.Duration, int, time.Duration) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok {
		remaining := s.capacity - 1
		s.buckets[key] = &bucket{tokens: remaining, lastSeen: now}
		if len(s.buckets)%64 == 0 {
			s.reapLocked(now)
		}
		if len(s.buckets) > s.maxKeys {
			s.evictLocked(now)
		}
		return true, 0, remaining, s.timeToFull(remaining, now, now)
	}

	if elapsed := now.Sub(b.lastSeen); elapsed > 0 {
		gained := int(elapsed/s.rate) * s.refill
		if gained > 0 {
			b.tokens += gained
			b.lastSeen = b.lastSeen.Add(time.Duration(gained/s.refill) * s.rate)
			if b.tokens >= s.capacity {
				b.tokens = s.capacity
				b.lastSeen = now
			}
		}
	}

	if b.tokens <= 0 {
		retry := max(s.rate-now.Sub(b.lastSeen), time.Second)
		return false, retry, 0, s.timeToFull(0, b.lastSeen, now)
	}
	b.tokens--
	return true, 0, b.tokens, s.timeToFull(b.tokens, b.lastSeen, now)
}

func (s *bucketStore) timeToFull(remaining int, lastSeen, now time.Time) time.Duration {
	deficit := s.capacity - remaining
	if deficit <= 0 {
		return 0
	}
	ticks := (deficit + s.refill - 1) / s.refill
	resetAt := lastSeen.Add(time.Duration(ticks) * s.rate)
	if d := resetAt.Sub(now); d > 0 {
		return d
	}
	return 0
}

func (s *bucketStore) tokensAt(b *bucket, now time.Time) int {
	elapsed := now.Sub(b.lastSeen)
	if elapsed <= 0 || s.rate <= 0 {
		return b.tokens
	}
	ticks := int64(elapsed / s.rate)
	if ticks >= int64(s.capacity) {
		return s.capacity
	}
	return min(b.tokens+int(ticks)*s.refill, s.capacity)
}

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

func (s *bucketStore) reapLocked(now time.Time) {
	for k, b := range s.buckets {
		if s.timeToFull(b.tokens, b.lastSeen, now) == 0 {
			delete(s.buckets, k)
		}
	}
}

func (s *bucketStore) evictLocked(now time.Time) {
	lowWater := s.maxKeys * 9 / 10
	if len(s.buckets) <= lowWater {
		return
	}
	type owingBucket struct {
		key    string
		tokens int
		owing  time.Duration
	}
	pending := make([]owingBucket, 0, len(s.buckets))
	for k, b := range s.buckets {
		pending = append(pending, owingBucket{
			key: k, tokens: s.tokensAt(b, now), owing: s.timeToFull(b.tokens, b.lastSeen, now),
		})
	}
	slices.SortFunc(pending, func(a, b owingBucket) int {
		if c := cmp.Compare(b.tokens, a.tokens); c != 0 {
			return c
		}
		return cmp.Compare(a.owing, b.owing)
	})
	for i := 0; i < len(pending) && len(s.buckets) > lowWater; i++ {
		delete(s.buckets, pending[i].key)
	}
}

func defaultRateLimitKey(r *http.Request) string {
	return clientip.RateKey(stripPort(r.RemoteAddr))
}

func newProxyAwareRateLimitKey(trusted clientip.Proxies) func(*http.Request) string {
	return func(r *http.Request) string {
		return clientip.RateKey(proxyAwareClient(r, trusted))
	}
}

func proxyAwareClient(r *http.Request, trusted clientip.Proxies) string {
	peer := stripPort(r.RemoteAddr)
	if trusted.Empty() || !trusted.ContainsAddr(peer) {
		return peer
	}
	if r.Header.Get("X-Forwarded-For") != "" {
		if ip, ok := clientip.Forwarded(r.Header, trusted); ok && isTrustablePublicIP(ip) {
			return ip.String()
		}
		return peer
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := clientip.ParseHop(xri); isTrustablePublicIP(ip) {
			return ip.String()
		}
	}
	return peer
}

func isTrustablePublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return true
}

func stripPort(addr string) string {
	if addr == "" {
		return addr
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
