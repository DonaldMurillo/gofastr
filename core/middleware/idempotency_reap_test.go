package middleware

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The memory store drops expired entries on a later Begin, as the SQL
// store's once-a-minute reap does.
func TestMemoryIdempotencyReapsExpired(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Millisecond).(*memoryIdempotencyStore)
	ctx := context.Background()
	for _, k := range []string{"a", "b", "c"} {
		s.Begin(ctx, k, "fp")
		s.Finish(ctx, k, "fp", &IdempotentResponse{Status: 200, Body: []byte("x")})
	}
	time.Sleep(20 * time.Millisecond)

	s.Begin(ctx, "inside-the-interval", "fp")
	if len(s.entries) != 4 {
		t.Fatalf("reap ran inside its interval: %d entries, want 4", len(s.entries))
	}
	s.lastReap = time.Now().Add(-2 * idemReapEvery)
	s.Begin(ctx, "after-the-interval", "fp")
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := s.entries[k]; ok {
			t.Errorf("expired entry %q is still resident after the reap", k)
		}
	}
	for _, k := range []string{"inside-the-interval", "after-the-interval"} {
		if _, ok := s.entries[k]; !ok {
			t.Errorf("live claim %q was reaped", k)
		}
	}
}

// SQLite compares expires_at as text, so the store binds UTC: a local
// offset that moves back (a DST fall-back) must not revive an expired claim.
func TestSQLIdempotencyExpiryIgnoresLocalOffset(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	time.Local = time.FixedZone("EDT", -4*3600)
	_, s := openSQLIdemStoreWithOpts(t,
		WithSQLIdempotencyInFlightTTL(50*time.Millisecond), WithSQLIdempotencyTTL(50*time.Millisecond))
	ctx := context.Background()
	if _, _, err := s.Begin(ctx, "claim", "fp"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(ctx, "done", "fp"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, "done", "fp", &IdempotentResponse{Status: 200}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(ctx, "claim", "fp"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("live claim: err = %v, want ErrInFlight", err)
	}

	time.Local = time.FixedZone("EST", -5*3600)
	time.Sleep(200 * time.Millisecond)
	for _, key := range []string{"claim", "done"} {
		if replay, ok, err := s.Begin(ctx, key, "fp"); err != nil || ok || replay != nil {
			t.Errorf("%s: expired row still live across the offset change: replay=%v ok=%v err=%v", key, replay, ok, err)
		}
	}
}
