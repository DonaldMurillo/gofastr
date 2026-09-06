package middleware

// Property, found by the 2026-09-05 adversarial red-probe round 4
// (family F19, fixed the same round with the maxKeys cap +
// evictIdleLocked): the number of resident rate-limit buckets must be
// bounded by an absolute cap, not by how many distinct keys an
// anonymous client can mint — the framework's own convention
// (framework/ratelimit maxKeys = 100_000, the idempotency store's
// defaultMaxIdemEntries = 100_000) is that an attacker-mintable keyed
// map gets a cap.
//
// Surfaces: core/middleware/ratelimit.go::bucketStore.take (the insert
// path — every request from a fresh source address inserts a bucket)
// and reapLocked (the only reclaim before the fix, which (a) ran
// solely on every 64th new-bucket insertion and (b) dropped only
// buckets idle > 5 minutes, so a sustained flood of fresh keys — every
// bucket younger than 5 minutes — evicted nothing). Reached through
// RateLimit's default KeyFunc: an IPv6 peer rotates source addresses
// without limit, one bucket per request.

import (
	"fmt"
	"testing"
	"time"
)

// TestRateLimitBucketStoreCapped floods the store with more distinct keys
// than the framework's own 100_000 maxKeys precedent and asserts the resident
// count stays under it. The flood runs far inside the 5-minute reap window, so
// only an absolute cap (not the idle reap) can satisfy the assertion.
func TestRateLimitBucketStoreCapped(t *testing.T) {
	const cap = 100_000 // framework/ratelimit maxKeys precedent
	s := newBucketStore(60, time.Minute, 60)

	for i := range cap + 5_000 {
		// Distinct per-source-address keys, exactly what defaultRateLimitKey
		// produces for a rotating IPv6 peer.
		s.take(fmt.Sprintf("2001:db8::%x:%x", i>>16, i&0xffff))
	}

	s.mu.Lock()
	got := len(s.buckets)
	s.mu.Unlock()
	if got > cap {
		t.Fatalf("SECURITY: [ratelimit-buckets] %d distinct-key requests left %d resident buckets (cap %d): the store has no absolute cap, and the 5-minute idle reap evicted nothing because every minted bucket is fresh — an anonymous IPv6 peer rotating source addresses grows the map until OOM",
			cap+5_000, got, cap)
	}
}
