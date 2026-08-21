package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestLimiter_AllowsUpToMaxThenBlocks pins the core sliding-window contract:
// exactly MaxAttempts requests pass, the MaxAttempts+1th is denied and reports
// a retryAfter equal to the configured BlockDuration.
func TestLimiter_AllowsUpToMaxThenBlocks(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 3, Window: time.Hour, BlockDuration: 30 * time.Minute})

	for i := 1; i <= 3; i++ {
		if ok, _ := rl.Allow("1.2.3.4"); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	ok, retry := rl.Allow("1.2.3.4")
	if ok {
		t.Fatal("attempt 4 must be blocked")
	}
	if retry <= 0 || retry > 30*time.Minute {
		t.Fatalf("retryAfter should be ~BlockDuration, got %v", retry)
	}
}

// TestLimiter_WindowResetsAttempts: attempts older than Window no longer count,
// so a caller under the threshold is never blocked by stale history.
func TestLimiter_WindowResetsAttempts(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 3, Window: 40 * time.Millisecond, BlockDuration: time.Hour})

	rl.Allow("k") // counts now
	time.Sleep(50 * time.Millisecond)
	// Window has elapsed; the stale attempt must not contribute to the count.
	for i := range 3 {
		if ok, _ := rl.Allow("k"); !ok {
			t.Fatalf("attempt after window reset should be allowed (i=%d)", i)
		}
	}
}

// TestLimiter_BlockExpires: once BlockDuration elapses the key is unblocked and
// allowed again (state is cleared, not just decremented).
func TestLimiter_BlockExpires(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Hour, BlockDuration: 40 * time.Millisecond})

	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("first attempt should pass")
	}
	if ok, _ := rl.Allow("k"); ok {
		t.Fatal("second attempt must be blocked")
	}
	time.Sleep(50 * time.Millisecond)
	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("should be allowed again after block expires")
	}
}

// TestLimiter_DevModeAdmitsAll: the dev-only short-circuit never denies,
// regardless of how many attempts fire.
func TestLimiter_DevModeAdmitsAll(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour, DevMode: true})
	for i := range 50 {
		if ok, _ := rl.Allow("k"); !ok {
			t.Fatalf("DevMode must admit every attempt (i=%d)", i)
		}
	}
}

// TestLimiter_StoreDelegates: when a Store is configured the limiter consults
// it instead of process memory, and a store error fails closed (denies).
func TestLimiter_StoreDelegates(t *testing.T) {
	var seen []string
	store := recordingStore{allow: true, seen: &seen}
	rl := NewLimiter(Config{MaxAttempts: 5, Window: time.Hour, BlockDuration: time.Hour, Store: store, Scope: "checkout"})

	if ok, _ := rl.Allow("9.9.9.9"); !ok {
		t.Fatal("store-backed allow must pass")
	}
	if len(seen) != 1 || seen[0] != "checkout|9.9.9.9" {
		t.Fatalf("store should see namespaced key %q, got %v", "checkout|9.9.9.9", seen)
	}

	rl2 := NewLimiter(Config{MaxAttempts: 5, Window: time.Hour, BlockDuration: time.Hour, Store: errorStore{}})
	if ok, _ := rl2.Allow("k"); ok {
		t.Fatal("store error must fail closed (deny)")
	}
}

// TestMiddleware_BlocksAfterMaxAndSetsRetryAfter is the general-purpose
// quickstart: wrap any handler, hammer one IP, the MaxAttempts+1th response is
// 429 carrying a Retry-After header.
func TestMiddleware_BlocksAfterMaxAndSetsRetryAfter(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 2, Window: time.Hour, BlockDuration: time.Minute})
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	hit := func(remote string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		h.ServeHTTP(rec, req)
		return rec
	}

	for i := range 2 {
		if rec := hit("5.5.5.5:1"); rec.Code != http.StatusOK {
			t.Fatalf("request %d should be 200, got %d", i, rec.Code)
		}
	}
	blocked := hit("5.5.5.5:1")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request from same IP must be 429, got %d", blocked.Code)
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Fatal("429 must carry a Retry-After header")
	}
}

// TestMiddleware_DifferentIPsAreIndependent: the default IP key gives each
// RemoteAddr its own budget.
func TestMiddleware_DifferentIPsAreIndependent(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Minute})
	h := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	hit := func(remote string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if hit("1.1.1.1:1") != 200 || hit("2.2.2.2:1") != 200 {
		t.Fatal("distinct IPs must each get their own first-request budget")
	}
	if hit("1.1.1.1:1") != 429 {
		t.Fatal("second hit from the same IP must be 429")
	}
}

// TestMiddlewareByKey_CustomKeyFunc pins the general-purpose escape hatch:
// group by any request-derived identity (API key, user id, route param) rather
// than IP.
func TestMiddlewareByKey_CustomKeyFunc(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Minute})
	byAPIKey := func(r *http.Request) string { return r.Header.Get("X-Api-Key") }
	h := rl.MiddlewareByKey(byAPIKey)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	hit := func(key string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Api-Key", key)
		// Same RemoteAddr for both, the key func, not IP, must drive the bucket.
		req.RemoteAddr = "7.7.7.7:1"
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if hit("alpha") != 200 || hit("beta") != 200 {
		t.Fatal("distinct api keys must each get their own budget")
	}
	if hit("alpha") != 429 {
		t.Fatal("second hit on the same api key must be 429")
	}
}

// TestClientIP_XFFHandling pins the proxy-trust posture: X-Forwarded-For is
// ignored by default and only honoured when trustXFF is true.
func TestClientIP_XFFHandling(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "9.9.9.9")
	r.RemoteAddr = "1.2.3.4:5678"

	if got := ClientIP(r, false); got != "1.2.3.4" {
		t.Fatalf("default must ignore XFF, got %q", got)
	}
	if got := ClientIP(r, true); got != "9.9.9.9" {
		t.Fatalf("trustXFF must use leftmost XFF entry, got %q", got)
	}
}

// TestLimiter_ConcurrentSafety hammers a single key from many goroutines. The
// invariant: Allow never panics and the total allowed count never exceeds
// MaxAttempts (the lock serializes the read-count-append). Run under -race to
// also pin the data-race freedom of the in-memory map.
func TestLimiter_ConcurrentSafety(t *testing.T) {
	rl := NewLimiter(Config{MaxAttempts: 100, Window: time.Hour, BlockDuration: time.Hour})

	const workers = 32
	const perWorker = 50
	var wg sync.WaitGroup
	wg.Add(workers)
	var allowed int64
	var mu sync.Mutex
	for range workers {
		go func() {
			defer wg.Done()
			for range perWorker {
				if ok, _ := rl.Allow("shared"); ok {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed > 100 {
		t.Fatalf("allowed=%d must not exceed MaxAttempts=100", allowed)
	}
}

// --- fake stores for the Store-contract test ---

type recordingStore struct {
	allow bool
	seen  *[]string
}

func (s recordingStore) Allow(_ context.Context, key string, _ Config) (bool, time.Duration, error) {
	*s.seen = append(*s.seen, key)
	return s.allow, 0, nil
}

type errorStore struct{}

func (errorStore) Allow(_ context.Context, _ string, _ Config) (bool, time.Duration, error) {
	return false, 0, fmt.Errorf("backend down")
}
