package middleware

// Property (2026-10-27 bug bash, P6): reclaiming a bucket never hands an
// exhausted key a fresh budget. lastSeen is the refill clock and moves
// only when a refill tick lands, not on a denied request, so the idle
// reap and the cap's eviction, both keyed on it, dropped a key that had
// spent a 1-per-hour budget and kept hammering, and its next request got
// a full bucket. Any traffic from other keys ran the reap, so an hourly
// login or signup limit became a five-minute one.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestReapKeepsExhaustedBucket exhausts a one-token-per-hour key, ages its
// refill clock past the five-minute idle cutoff (no refill has landed),
// and triggers the reap with 63 other keys. The key must stay denied.
func TestReapKeepsExhaustedBucket(t *testing.T) {
	s := newBucketStore(1, time.Hour, 1)
	if ok, _, _, _ := s.take("attacker"); !ok {
		t.Fatal("first request denied")
	}
	s.mu.Lock()
	s.buckets["attacker"].lastSeen = time.Now().Add(-6 * time.Minute)
	s.mu.Unlock()

	for i := range 63 {
		s.take(fmt.Sprintf("other-%d", i))
	}
	if ok, _, _, _ := s.take("attacker"); ok {
		t.Fatal("SECURITY: [ratelimit-reap] an exhausted 1/hour bucket was reaped after 6 minutes and came back full")
	}
}

// TestReapDropsFullBucket pins the reclaim that stays: a bucket whose
// refill has caught up holds nothing a fresh bucket would not, so the
// reap drops it.
func TestReapDropsFullBucket(t *testing.T) {
	s := newBucketStore(2, time.Minute, 1)
	s.take("idle")
	s.mu.Lock()
	s.buckets["idle"].lastSeen = time.Now().Add(-2 * time.Minute)
	s.mu.Unlock()

	for i := range 63 {
		s.take(fmt.Sprintf("other-%d", i))
	}
	s.mu.Lock()
	_, kept := s.buckets["idle"]
	s.mu.Unlock()
	if kept {
		t.Fatal("a bucket back at capacity survived the reap")
	}
}

// TestEvictKeepsExhaustedBucket fills a capped store past its cap after
// one key spent its whole budget. The flood's buckets each still hold a
// token, so shedding them loses less than shedding the exhausted key,
// which must stay denied.
func TestEvictKeepsExhaustedBucket(t *testing.T) {
	s := newBucketStoreCapped(2, time.Hour, 1, 10)
	s.take("attacker")
	s.take("attacker")
	if ok, _, _, _ := s.take("attacker"); ok {
		t.Fatal("third request inside the budget allowed")
	}
	for i := range 20 {
		s.take(fmt.Sprintf("flood-%d", i))
	}
	if ok, _, _, _ := s.take("attacker"); ok {
		t.Fatal("SECURITY: [ratelimit-evict] the cap's eviction shed an exhausted bucket before the flood's fuller ones")
	}
}

// TestEvictKeepsExhaustedBucketDefaultRefill is the same property under
// the default RefillBy (== Capacity, the 60/60 zero config). The shed
// ranked by time to full, which is counted in refill ticks; when one
// tick refills the whole bucket an exhausted key and a one-token-spent
// flood key both owe one tick, and the older one, the exhausted key that
// was there first, owed less and was shed first, coming back with a
// full budget. With a per-username KeyFunc a spray of junk usernames
// reset a victim account's login budget. The shed now ranks by tokens
// left first.
func TestEvictKeepsExhaustedBucketDefaultRefill(t *testing.T) {
	h := RateLimit(RateLimitConfig{
		Capacity:    2,
		RefillEvery: time.Hour, // RefillBy left 0: defaults to Capacity
		MaxKeys:     10,
		KeyFunc:     func(r *http.Request) string { return r.Header.Get("X-User") },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	hit := func(user string) int {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.Header.Set("X-User", user)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	hit("alice")
	hit("alice")
	if code := hit("alice"); code != http.StatusTooManyRequests {
		t.Fatalf("setup: third request should be limited, got %d", code)
	}
	for i := range 20 {
		hit(fmt.Sprintf("junk-%d", i))
	}
	if code := hit("alice"); code != http.StatusTooManyRequests {
		t.Fatalf("SECURITY: [ratelimit-evict] exhausted key got a fresh budget (status %d) after a junk-key flood under the default RefillBy", code)
	}
}

// TestEvictRanksRefilledTokens: the tokens rank counts the refill ticks
// that have landed, not the stored count. A key that spent its budget
// long ago is full again and loses nothing when shed; ranking it by its
// stored zero kept it and shed the flood's live buckets instead.
func TestEvictRanksRefilledTokens(t *testing.T) {
	s := newBucketStoreCapped(2, time.Minute, 1, 10)
	for i := range 10 {
		s.take(fmt.Sprintf("k-%d", i))
	}
	s.mu.Lock()
	s.buckets["k-0"].tokens, s.buckets["k-0"].lastSeen = 0, time.Now().Add(-10*time.Minute)
	s.mu.Unlock()
	s.take("k-10") // 11 > cap: shed two, down to 9
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, kept := s.buckets["k-0"]; kept {
		t.Fatal("the shed kept a bucket the refill has made full and dropped live ones")
	}
}

// TestEvictShedsFullBucketsFirst checks the order inside the shed: a
// bucket back at capacity goes first, even when buckets with an older
// refill clock still owe tokens. Sorting on the refill clock alone sheds
// those owing buckets instead.
func TestEvictShedsFullBucketsFirst(t *testing.T) {
	s := newBucketStoreCapped(2, time.Minute, 1, 10)
	for i := range 10 {
		s.take(fmt.Sprintf("k-%d", i))
	}
	now := time.Now()
	s.mu.Lock()
	// k-0 and k-1 spent both tokens and are 80-90s into recovering
	// them; k-2 owes one token whose tick landed 10s ago, so it is full.
	s.buckets["k-0"].tokens, s.buckets["k-0"].lastSeen = 0, now.Add(-90*time.Second)
	s.buckets["k-1"].tokens, s.buckets["k-1"].lastSeen = 0, now.Add(-80*time.Second)
	s.buckets["k-2"].lastSeen = now.Add(-70 * time.Second)
	s.mu.Unlock()
	s.take("k-10") // 11 > cap: shed two, down to 9

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, kept := s.buckets["k-2"]; kept {
		t.Fatal("the shed kept a full bucket and dropped buckets that still owe tokens")
	}
	if len(s.buckets) != 9 {
		t.Fatalf("resident buckets = %d, want the low-water mark 9", len(s.buckets))
	}
}
