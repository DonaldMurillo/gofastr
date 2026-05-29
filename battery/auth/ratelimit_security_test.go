package auth

import (
	"testing"
	"time"
)

// Property: the rate limiter must not let attacker-chosen keys grow process
// memory without bound. Distinct non-existent login emails (or rotated IPs)
// each insert a state; without eviction the map grows until OOM.
func TestRateLimiter_BoundedUnderKeyFlood(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		MaxAttempts:   3,
		Window:        50 * time.Millisecond,
		BlockDuration: 50 * time.Millisecond,
	})

	// Flood with far more distinct keys than the cap allows.
	const flood = maxRateLimitKeys * 3
	for i := 0; i < flood; i++ {
		rl.Allow(uniqueKey(i))
	}

	rl.mu.Lock()
	n := len(rl.states)
	rl.mu.Unlock()

	if n >= flood {
		t.Fatalf("limiter map grew unbounded: %d entries for %d distinct keys", n, flood)
	}
	if n > maxRateLimitKeys {
		t.Fatalf("limiter map exceeded cap: %d entries, cap %d", n, maxRateLimitKeys)
	}
}

// Idle states (block elapsed, attempts aged out of the window) must be
// reclaimed rather than pinned forever — one-shot probes shouldn't leak.
func TestRateLimiter_EvictsIdleStates(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		MaxAttempts:   3,
		Window:        20 * time.Millisecond,
		BlockDuration: 20 * time.Millisecond,
	})

	for i := 0; i < 1000; i++ {
		rl.Allow(uniqueKey(i))
	}
	// Let every attempt age out of the window.
	time.Sleep(40 * time.Millisecond)

	// A single fresh call triggers the lazy sweep; the stale singletons go.
	rl.Allow("trigger-sweep")

	rl.mu.Lock()
	n := len(rl.states)
	rl.mu.Unlock()

	if n > 50 {
		t.Fatalf("idle states not reclaimed: %d entries remain after window elapsed", n)
	}
}

// An active block must still be honoured even while the limiter is shedding
// idle keys — eviction must not become a block-bypass primitive.
func TestRateLimiter_BlockSurvivesEviction(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		MaxAttempts:   2,
		Window:        time.Hour,
		BlockDuration: time.Hour,
	})

	// Trip the block for the victim key.
	rl.Allow("victim")
	rl.Allow("victim")
	if allowed, _ := rl.Allow("victim"); allowed {
		t.Fatalf("victim key should be blocked after exceeding MaxAttempts")
	}

	// Flood with unrelated keys to drive eviction.
	for i := 0; i < 100_000; i++ {
		rl.Allow(uniqueKey(i))
	}

	// The victim must remain blocked, not silently freed by the sweep.
	if allowed, _ := rl.Allow("victim"); allowed {
		t.Fatalf("eviction released an active block — DoS-control bypass")
	}
}

func uniqueKey(i int) string {
	// Deterministic distinct keys without allocating a sprintf each time
	// in hot loops is fine here; readability over micro-optimisation.
	const hex = "0123456789abcdef"
	var b [8]byte
	for j := 0; j < 8; j++ {
		b[j] = hex[(i>>(uint(j)*4))&0xF]
	}
	return "account:" + string(b[:])
}
