package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestLoad_LoginEndpoint_RateLimiterHoldsUnderConcurrency is the
// Go-native equivalent of `vegeta attack -rate 1000 -duration 30s`. It
// throws K concurrent goroutines at /auth/login, each issuing requests
// in a tight loop for a short bursty window, and verifies:
//
//   - The per-IP limiter eventually 429s (the budget is small relative
//     to the burst — otherwise the burst would pass the limiter and
//     the limiter wouldn't be doing its job).
//   - At least the configured MaxAttempts requests are admitted (the
//     limiter doesn't fail closed on first request).
//   - Goroutine count returns to baseline after the burst completes.
//   - No requests succeed (every login uses a wrong password) — pins
//     "rate limit returning 429" vs "wrong-password 401".
//
// Realistic numbers: the suite already takes ~13s and we don't want to
// slow it down 3x. The burst is 200 requests across 16 workers — small
// enough to keep wall-clock under 1s, large enough to exercise the
// limiter's sliding window. The full vegeta / k6 multi-RPS test
// belongs in CI-only soak runs.
func TestLoad_LoginEndpoint_RateLimiterHoldsUnderConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load-style test in -short mode")
	}

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newMemoryUserStore(),
		LoginRateLimit: &RateLimiterConfig{
			MaxAttempts:   20,
			Window:        time.Minute,
			BlockDuration: time.Minute,
		},
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"email": "x@x.test", "password": "wrong"})

	const (
		workers           = 16
		requestsPerWorker = 50 // → 800 total
	)
	var (
		got429, got401, gotOther int32
		wg                       sync.WaitGroup
	)
	preGoroutines := runtime.NumGoroutine()

	start := make(chan struct{})
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			<-start
			for i := 0; i < requestsPerWorker; i++ {
				resp, err := client.Post(srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
				if err != nil {
					t.Errorf("worker request: %v", err)
					return
				}
				switch resp.StatusCode {
				case http.StatusTooManyRequests:
					atomic.AddInt32(&got429, 1)
				case http.StatusUnauthorized:
					atomic.AddInt32(&got401, 1)
				default:
					atomic.AddInt32(&gotOther, 1)
					t.Errorf("unexpected status: %d", resp.StatusCode)
				}
				resp.Body.Close()
			}
		}()
	}
	close(start)
	wg.Wait()

	total := int(got429 + got401 + gotOther)
	if total != workers*requestsPerWorker {
		t.Fatalf("expected %d total responses, got %d", workers*requestsPerWorker, total)
	}
	if got429 == 0 {
		t.Fatalf("expected the per-IP limiter to 429 some requests under %d-burst; got 0 (the limiter is broken)", total)
	}
	if got401 < int32(20) {
		t.Fatalf("expected at least MaxAttempts=20 401-admitted requests; got %d (limiter is over-aggressive)", got401)
	}

	// Goroutine sanity: allow the http transport some slack but a
	// blown-up server would leak hundreds.
	time.Sleep(100 * time.Millisecond) // give httptest a moment to settle
	postGoroutines := runtime.NumGoroutine()
	if postGoroutines > preGoroutines+50 {
		t.Fatalf("possible goroutine leak: pre=%d post=%d (delta=%d)",
			preGoroutines, postGoroutines, postGoroutines-preGoroutines)
	}
	t.Logf("load test: %d total, %d=429, %d=401, goroutine delta=%d",
		total, got429, got401, postGoroutines-preGoroutines)
}

// TestLoad_OAuthStateNonces_BoundedUnderRedirectStorm pins that the
// new stateless OAuth state design does not grow the in-process memory
// footprint as a function of redirect volume. Pre-stateless, every
// /oauth/{provider} request inserted a row into stateStore — an
// attacker could blow up RAM by spamming redirects. After this PR,
// generateState writes nothing to the plugin, and the usedNonces map
// only grows on successful callbacks (which require a valid HMAC + a
// not-yet-expired token).
func TestLoad_OAuthStateNonces_BoundedUnderRedirectStorm(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"mock": &mockProvider{name: "mock"}},
		StateSecret: "load-test-key",
	})

	const N = 5000
	for i := 0; i < N; i++ {
		if _, err := p.generateState("mock"); err != nil {
			t.Fatalf("generateState %d: %v", i, err)
		}
	}

	// generateState must not touch the nonces map.
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	if got := len(p.usedNonces); got != 0 {
		t.Fatalf("usedNonces must stay empty after %d redirects; got %d entries", N, got)
	}
}

// TestLoad_OAuthStateNonces_GarbageCollectsExpired pins that the
// callback path's nonce map self-purges past nonceGCThreshold so a
// long-running process can't drift to OOM under steady-state callback
// volume.
func TestLoad_OAuthStateNonces_GarbageCollectsExpired(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"mock": &mockProvider{name: "mock"}},
		StateSecret: "gc-test-key",
	})

	// Plant > threshold expired entries.
	p.noncesMu.Lock()
	past := time.Now().Add(-time.Hour)
	for i := 0; i < nonceGCThreshold+200; i++ {
		p.usedNonces["expired-"+stateTestB64.EncodeToString([]byte{byte(i), byte(i >> 8)})] = past
	}
	expiredCount := len(p.usedNonces)
	p.noncesMu.Unlock()

	if expiredCount < nonceGCThreshold {
		t.Fatalf("test precondition: expected to plant >= %d entries, got %d", nonceGCThreshold, expiredCount)
	}

	// Now trigger the GC by validating one fresh state — that path is
	// where the size-gated purge runs.
	fresh, err := p.generateState("mock")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if !p.validateAndConsumeState(fresh, "mock") {
		t.Fatal("fresh validate failed")
	}

	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	if got := len(p.usedNonces); got >= expiredCount {
		t.Fatalf("expected GC to drop expired entries; before=%d after=%d", expiredCount, got)
	}
}
