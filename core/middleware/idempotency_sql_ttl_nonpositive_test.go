package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"
)

// WithSQLIdempotencyInFlightTTL(0) and WithSQLIdempotencyTTL(0) stored the
// zero. The in-flight claim was then written already expired, so a retry
// arriving while the first request still ran took the claim over and ran
// the handler a second time; a zero response TTL made the cached response
// expire on write, so the retry re-executed too. A negative TTL did the
// same. A non-positive TTL now keeps the default (30s in flight, 24h).
func TestSQLIdempotencyNonPositiveTTLKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Minute} {
		t.Run("inflight/"+d.String(), func(t *testing.T) {
			s := openSQLIdemStoreOpts(t, WithSQLIdempotencyInFlightTTL(d))
			ctx := context.Background()
			if _, _, err := s.Begin(ctx, "k", "fp"); err != nil {
				t.Fatalf("claim: %v", err)
			}
			// The first request is still running: a retry must be told so.
			if _, _, err := s.Begin(ctx, "k", "fp"); !errors.Is(err, ErrInFlight) {
				t.Fatalf("retry during the first request: err = %v, want ErrInFlight (the claim expired on write)", err)
			}
		})
		t.Run("ttl/"+d.String(), func(t *testing.T) {
			s := openSQLIdemStoreOpts(t, WithSQLIdempotencyTTL(d))
			ctx := context.Background()
			if _, _, err := s.Begin(ctx, "k", "fp"); err != nil {
				t.Fatalf("claim: %v", err)
			}
			if err := s.Finish(ctx, "k", "fp", &IdempotentResponse{Status: 201, Header: http.Header{}, Body: []byte("ok")}); err != nil {
				t.Fatalf("finish: %v", err)
			}
			got, replay, err := s.Begin(ctx, "k", "fp")
			if err != nil || !replay || got == nil {
				t.Fatalf("retry after finish: replay = %v, err = %v; want the cached response (it expired on write)", replay, err)
			}
		})
	}
}

func openSQLIdemStoreOpts(t *testing.T, opts ...SQLIdempotencyOption) *SQLIdempotencyStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLIdempotencyStore(db, opts...)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}
