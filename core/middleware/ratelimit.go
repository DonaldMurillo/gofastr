package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitConfig controls the in-memory token-bucket rate limiter.
//
// KeyFunc selects the per-bucket identity (per IP, per session, per API key,
// etc.). It defaults to a remote-address extractor that handles
// X-Forwarded-For headers safely (trusts the leftmost entry).
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
		cfg.KeyFunc = defaultRateLimitKey
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
			allowed, retryAfter := buckets.take(key)
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
// (allowed, retryAfter); retryAfter is the duration until the bucket would
// have at least one token, or 0 on the success path.
func (s *bucketStore) take(key string) (bool, time.Duration) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[key]
	if !ok {
		// Fresh bucket starts full.
		s.buckets[key] = &bucket{tokens: s.capacity - 1, lastSeen: now}
		// Opportunistic reap of stale buckets so the map doesn't grow.
		if len(s.buckets)%64 == 0 {
			s.reapLocked(now)
		}
		return true, 0
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
		// How long until next token? rate / refill per token, rounded up.
		perToken := s.rate / time.Duration(s.refill)
		retry := perToken - now.Sub(b.lastSeen)
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	b.tokens--
	return true, 0
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

// defaultRateLimitKey extracts a per-client identity from the request.
// Trusts the leftmost X-Forwarded-For entry when present; otherwise uses
// r.RemoteAddr. Strips the port so /key matching is stable.
func defaultRateLimitKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take just the first hop.
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
