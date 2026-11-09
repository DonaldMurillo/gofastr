package middleware

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The optional InFlightTTLStore capability: both built-in stores report
// how long an in-flight claim holds a key, so the App can size and check
// it against the request budget at startup (#659, fifth decision).

func TestMemoryIdempotencyInFlightTTLOption(t *testing.T) {
	def := NewMemoryIdempotencyStore(time.Hour)
	if got := def.(InFlightTTLStore).InFlightTTL(); got != 30*time.Second {
		t.Fatalf("default in-flight TTL = %v, want 30s", got)
	}
	sized := NewMemoryIdempotencyStore(time.Hour, WithMemoryIdempotencyInFlightTTL(5*time.Minute))
	if got := sized.(InFlightTTLStore).InFlightTTL(); got != 5*time.Minute {
		t.Fatalf("in-flight TTL = %v, want 5m", got)
	}
	for _, d := range []time.Duration{0, -time.Second} {
		if got := NewMemoryIdempotencyStore(time.Hour, WithMemoryIdempotencyInFlightTTL(d)).(InFlightTTLStore).InFlightTTL(); got != 30*time.Second {
			t.Fatalf("in-flight TTL with %v = %v, want the 30s default kept", d, got)
		}
	}
}

// An expired in-flight claim stops answering ErrInFlight, so a retry
// re-executes once the claim's own TTL has passed.
func TestMemoryIdempotencyInFlightClaimExpires(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryIdempotencyInFlightTTL(40*time.Millisecond))
	ctx := context.Background()
	if _, _, err := s.Begin(ctx, "k", "fp"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Begin(ctx, "k", "fp"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("claim in flight: %v, want ErrInFlight", err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, _, err := s.Begin(ctx, "k", "fp"); err != nil {
		t.Fatalf("claim after its TTL: %v, want a fresh claim", err)
	}
}

func TestSQLIdempotencyStoreReportsInFlightTTL(t *testing.T) {
	_, s := openSQLIdemStore(t)
	if got := s.InFlightTTL(); got != 30*time.Second {
		t.Fatalf("default in-flight TTL = %v, want 30s", got)
	}
	_, sized := openSQLIdemStoreWithOpts(t, WithSQLIdempotencyInFlightTTL(2*time.Minute))
	if got := sized.InFlightTTL(); got != 2*time.Minute {
		t.Fatalf("in-flight TTL = %v, want 2m", got)
	}
}
