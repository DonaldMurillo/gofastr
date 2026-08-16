package ratelimit

import (
	"strings"
	"testing"
	"time"
)

func TestLimiterBoundsRetainedKeyBytes(t *testing.T) {
	rl := NewLimiter(Config{
		MaxAttempts:   3,
		Window:        time.Hour,
		BlockDuration: time.Hour,
	})
	key := strings.Repeat("x", 1<<20)

	if allowed, _ := rl.Allow(key); !allowed {
		t.Fatal("first request was denied")
	}

	rl.mu.Lock()
	_, retainedRawKey := rl.states[key]
	rl.mu.Unlock()
	if retainedRawKey {
		t.Fatalf("limiter retained an attacker-controlled %d-byte map key", len(key))
	}
}

func TestKeyFloodKeepsActiveBlocks(t *testing.T) {
	rl := NewLimiter(Config{
		MaxAttempts:   1,
		Window:        time.Hour,
		BlockDuration: time.Hour,
	})

	if allowed, _ := rl.Allow("victim"); !allowed {
		t.Fatal("victim's first request was denied")
	}
	if allowed, _ := rl.Allow("victim"); allowed {
		t.Fatal("victim was not blocked")
	}

	for i := range maxKeys - 1 {
		key := uniqueKey(i)
		rl.Allow(key)
		rl.Allow(key)
	}

	if allowed, _ := rl.Allow("victim"); allowed {
		t.Fatal("key flood evicted the victim's active block")
	}
}

// A block whose duration has elapsed is not a lockout, but it keeps a
// non-zero blockedUntil until that key is touched again — AllowContext
// clears it lazily, and a key nobody touches again is never cleared.
//
// That stale timestamp is read twice by eviction, and it was wrong both
// times: the entry survived the idle sweep (its attempts are still inside
// Window, so it looks busy) and then sorted among the ACTIVE blocks, where
// its older timestamp ranked it as more protected than a live lockout.
//
// Driving this through Allow would need two different BlockDurations in one
// limiter, which the config cannot express — so the state is built directly
// and evictLocked called once. That also keeps the test off the 100k-key
// flood path.
func TestElapsedBlockIsReclaimed(t *testing.T) {
	rl := NewLimiter(Config{
		MaxAttempts:   1,
		Window:        time.Hour,
		BlockDuration: time.Hour,
	})
	now := time.Now()

	// Elapsed block, but with an attempt still inside Window so the idle
	// sweep sees a busy key rather than an idle one.
	rl.states["stale"] = &rlState{
		attempts:     []time.Time{now.Add(-time.Minute)},
		blockedUntil: now.Add(-time.Minute),
		blockOrder:   1,
	}
	// A genuinely live lockout.
	rl.states["victim"] = &rlState{
		attempts:     []time.Time{now},
		blockedUntil: now.Add(time.Hour),
		blockOrder:   2,
	}

	rl.mu.Lock()
	rl.evictLocked(now)
	rl.mu.Unlock()

	if _, ok := rl.states["stale"]; ok {
		t.Error("an elapsed block was retained and ranked among active blocks")
	}
	if _, ok := rl.states["victim"]; !ok {
		t.Error("eviction dropped a live block")
	}
}
