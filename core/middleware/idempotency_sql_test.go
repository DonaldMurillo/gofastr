package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func openSQLIdemStore(t *testing.T) (*sql.DB, *SQLIdempotencyStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLIdempotencyStore(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return db, s
}

func TestSQLIdempotency_FreshClaimAndReplay(t *testing.T) {
	_, s := openSQLIdemStore(t)
	ctx := context.Background()

	// Fresh claim.
	resp, ok, err := s.Begin(ctx, "k1", "fp1")
	if err != nil || ok || resp != nil {
		t.Fatalf("fresh claim: %v %v %v", resp, ok, err)
	}

	// While in flight, second call returns ErrInFlight.
	if _, _, err := s.Begin(ctx, "k1", "fp1"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("expected ErrInFlight, got %v", err)
	}

	// Finish stores the response.
	cached := &IdempotentResponse{
		Status: 201,
		Header: http.Header{"X-Custom": []string{"v"}},
		Body:   []byte(`{"id":1}`),
	}
	if err := s.Finish(ctx, "k1", cached); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// Replay returns the cached response.
	got, ok, err := s.Begin(ctx, "k1", "fp1")
	if err != nil || !ok || got == nil {
		t.Fatalf("replay: %v %v %v", got, ok, err)
	}
	if got.Status != 201 || string(got.Body) != `{"id":1}` || got.Header.Get("X-Custom") != "v" {
		t.Fatalf("replay round-trip lost data: %+v", got)
	}
}

func TestSQLIdempotency_FingerprintMismatch(t *testing.T) {
	_, s := openSQLIdemStore(t)
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "k", "fp1")
	_ = s.Finish(ctx, "k", &IdempotentResponse{Status: 200, Header: http.Header{}, Body: []byte("ok")})

	_, _, err := s.Begin(ctx, "k", "different-fp")
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("expected ErrFingerprintMismatch, got %v", err)
	}
}

func TestSQLIdempotency_FinishNilReleasesClaim(t *testing.T) {
	_, s := openSQLIdemStore(t)
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "k", "fp")
	if err := s.Finish(ctx, "k", nil); err != nil {
		t.Fatalf("finish nil: %v", err)
	}
	// Subsequent call should be a fresh claim, not in-flight.
	resp, ok, err := s.Begin(ctx, "k", "fp")
	if err != nil || ok || resp != nil {
		t.Fatalf("after release: %v %v %v", resp, ok, err)
	}
}

func TestSQLIdempotency_ExpiryAllowsFreshClaim(t *testing.T) {
	db, _ := openSQLIdemStore(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTTL(20*time.Millisecond))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "k", "fp")
	_ = s.Finish(ctx, "k", &IdempotentResponse{Status: 200, Header: http.Header{}, Body: []byte("ok")})
	time.Sleep(40 * time.Millisecond)
	resp, ok, err := s.Begin(ctx, "k", "fp")
	if err != nil || ok || resp != nil {
		t.Fatalf("expected fresh claim post-expiry, got %v %v %v", resp, ok, err)
	}
}

func TestSQLIdempotency_UnsafeTableNameRejected(t *testing.T) {
	db, _ := openSQLIdemStore(t)
	if _, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable("bad name; DROP")); err == nil {
		t.Fatal("expected error on unsafe table name")
	}
}

// Simulates the race where two concurrent transactions both see "no row"
// and both attempt INSERT — the second must surface as ErrInFlight, not a
// raw PK-violation error that the middleware would interpret as fail-open.
func TestSQLIdempotency_DoubleInsertIsErrInFlightNotRawError(t *testing.T) {
	db, s := openSQLIdemStore(t)
	ctx := context.Background()
	// Stand in for the "first writer" tx: directly insert an in-flight
	// row outside Begin. Begin's own insert path must now see that row
	// instead of crashing with a UNIQUE constraint failure.
	now := time.Now()
	_, err := db.Exec(
		"INSERT INTO idempotency_keys (key, fingerprint, expires_at, created_at) VALUES (?, ?, ?, ?)",
		"k-concurrent", "fp1", now.Add(30*time.Second), now,
	)
	if err != nil {
		t.Fatalf("seed in-flight row: %v", err)
	}
	// Same fingerprint, fresh Begin — should see the in-flight row.
	_, _, err = s.Begin(ctx, "k-concurrent", "fp1")
	if !errors.Is(err, ErrInFlight) {
		t.Fatalf("expected ErrInFlight, got %v", err)
	}
}
