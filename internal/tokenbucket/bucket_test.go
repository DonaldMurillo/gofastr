package tokenbucket

import (
	"testing"
	"time"
)

// The bucket starts at its burst and refills at rate tokens a second.
func TestBucketRefillsAtRate(t *testing.T) {
	f := New(4)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 8 {
		if !f.Allow(t0) {
			t.Fatalf("token %d refused inside the burst", i)
		}
	}
	if f.Allow(t0) {
		t.Fatal("token past the burst admitted with no time elapsed")
	}
	half := t0.Add(500 * time.Millisecond)
	if !f.Allow(half) || !f.Allow(half) || f.Allow(half) {
		t.Fatal("half a second at 4/s should refill exactly 2 tokens")
	}
	if !f.Allow(t0.Add(time.Hour)) {
		t.Fatal("an idle hour refilled nothing")
	}
}
