package middleware

import (
	"testing"
	"time"
)

// A bucket that sat full restarts its refill clock at the first spend:
// Capacity is the peak burst, however long the key was quiet.
func TestRateLimitFullBucketRestartsClock(t *testing.T) {
	cases := []struct {
		name             string
		capacity, refill int
		quiet            time.Duration
	}{
		{"default refill, tick about to land", 60, 60, 119900 * time.Millisecond},
		{"one token per tick", 5, 1, 10*time.Minute - 100*time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newBucketStore(tc.capacity, time.Minute, tc.refill)
			s.take("k")
			s.buckets["k"].lastSeen = time.Now().Add(-tc.quiet)
			allowed := 0
			for range tc.capacity {
				if ok, _, _, _ := s.take("k"); ok {
					allowed++
				}
			}
			time.Sleep(150 * time.Millisecond) // past the stale clock's next tick
			for range tc.capacity {
				if ok, _, _, _ := s.take("k"); ok {
					allowed++
				}
			}
			if allowed != tc.capacity {
				t.Fatalf("allowed %d in one burst, want Capacity %d", allowed, tc.capacity)
			}
		})
	}
}

// A bucket still owing tokens keeps its tick clock, so partial refills land on time.
func TestRateLimitPartialRefillKeepsClock(t *testing.T) {
	s := newBucketStore(5, time.Minute, 1)
	for range 5 {
		s.take("k")
	}
	s.buckets["k"].lastSeen = time.Now().Add(-90 * time.Second) // one tick landed, half of the next
	if ok, _, _, _ := s.take("k"); !ok {
		t.Fatal("the landed tick's token was refused")
	}
	if since := time.Since(s.buckets["k"].lastSeen); since < 29*time.Second || since > 31*time.Second {
		t.Fatalf("tick clock moved: %v into the tick, want about 30s", since)
	}
}
