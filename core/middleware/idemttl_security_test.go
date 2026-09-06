package middleware

import (
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. Both idempotency construction surfaces
// tested their TTL `<= 0` and substituted 24h, so a negative TTL (sign or
// unit error) silently granted the LONGEST response retention the
// middleware offers instead of the shortest the caller asked for.
// Surfaces: Idempotency (IdempotencyConfig.TTL),
// NewMemoryIdempotencyStore (ttl).
// Pins both constructors folding TTL <= 0 onto the 24h default, found
// by the 2026-09-04 red-probe round; fixed in both panicking at
// construction for TTL < 0 (neither returns an error) while 0 keeps the
// default.

func TestIdempotencyNegativeTTLPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Idempotency: negative TTL silently folded onto the 24h default")
		}
	}()
	_ = Idempotency(IdempotencyConfig{TTL: -time.Minute})
}

func TestNewMemoryIdemStoreNegativeTTLPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewMemoryIdempotencyStore: negative ttl silently folded onto the 24h default")
		}
	}()
	_ = NewMemoryIdempotencyStore(-time.Minute)
}

func TestIdemTTLZeroKeepsDefault(t *testing.T) {
	if s := NewMemoryIdempotencyStore(0); s == nil {
		t.Fatal("zero ttl must keep the 24h default, not fail construction")
	}
	// Idempotency with TTL unset is the documented default path pinned by
	// the rest of the suite; here it must at least not panic.
	mw := Idempotency(IdempotencyConfig{})
	if mw == nil {
		t.Fatal("zero TTL must keep the 24h default, not fail construction")
	}
}
