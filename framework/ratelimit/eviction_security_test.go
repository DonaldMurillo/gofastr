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
