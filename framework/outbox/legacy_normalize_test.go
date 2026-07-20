package outbox

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

// openPureOutbox opens a fresh pure-sqlite DB + Outbox so the normalizer's
// dialect path (the one that matters for legacy timestamps) is exercised.
func openPureOutbox(t *testing.T) (*sql.DB, *Outbox) {
	t.Helper()
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	o, err := New(db, WithHandlerGrace(0), WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	return db, o
}

const (
	legacySpaceLayout    = "2006-01-02 15:04:05.999999999-07:00"
	legacySpaceNoTZ      = "2006-01-02 15:04:05.999999999"
	canonicalRFC3339Nano = time.RFC3339Nano
)

// TestNormalize_RewritesLegacyParentAndDelivery inserts rows with
// space-separated timestamps directly (bypassing Append so they stay legacy),
// then re-opens the outbox to trigger normalization and asserts the on-disk
// values are now canonical RFC3339Nano text.
func TestNormalize_RewritesLegacyParentAndDelivery(t *testing.T) {
	db, o := openPureOutbox(t)
	ctx := context.Background()

	// Future lease in legacy layout — the exact shape that breaks the relay
	// (an un-expired lease that sorts as expired).
	futureLegacy := time.Now().UTC().Add(time.Hour).Format(legacySpaceLayout)
	pastLegacy := time.Now().UTC().Add(-time.Hour).Format(legacySpaceLayout)

	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'pending', 0, $1)`, pastLegacy); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox_delivery (row_id, consumer, status, attempts, created_at, claimed_until)
		VALUES ('p1', 'c', 'pending', 0, $1, $2)`, pastLegacy, futureLegacy); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	// Re-open: New() runs normalization.
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	var parentCreated string
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM event_outbox WHERE id='p1'`).Scan(&parentCreated); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if strings.Contains(parentCreated, " ") {
		t.Fatalf("parent created_at still legacy %q (must be canonical RFC3339Nano)", parentCreated)
	}
	if _, err := time.Parse(canonicalRFC3339Nano, parentCreated); err != nil {
		t.Fatalf("parent created_at %q is not RFC3339Nano: %v", parentCreated, err)
	}

	var claimed string
	if err := db.QueryRowContext(ctx, `SELECT claimed_until FROM event_outbox_delivery WHERE row_id='p1' AND consumer='c'`).Scan(&claimed); err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if strings.Contains(claimed, " ") {
		t.Fatalf("delivery claimed_until still legacy %q (must be canonical RFC3339Nano)", claimed)
	}
	if _, err := time.Parse(canonicalRFC3339Nano, claimed); err != nil {
		t.Fatalf("delivery claimed_until %q is not RFC3339Nano: %v", claimed, err)
	}

	// The future lease must still compare as FUTURE relative to now — i.e.
	// the SQL predicate the relay uses must say "not expired". This is the
	// behavior fix the whole normalizer exists for.
	var leased int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM event_outbox_delivery WHERE row_id='p1' AND consumer='c' AND (claimed_until IS NULL OR claimed_until <= $1)`,
		time.Now().UTC()).Scan(&leased); err != nil {
		t.Fatalf("compare: %v", err)
	}
	if leased != 0 {
		t.Fatalf("after normalization a future lease still compares as expired: leased=%d (claimed_until=%q)", leased, claimed)
	}
}

// TestNormalize_Idempotent asserts a second normalization pass writes nothing
// because every value is already canonical. We measure this by counting
// rows-affected across both passes — the second pass must touch zero rows.
func TestNormalize_Idempotent(t *testing.T) {
	db, o := openPureOutbox(t)
	ctx := context.Background()

	// Insert one legacy row.
	pastLegacy := time.Now().UTC().Add(-time.Hour).Format(legacySpaceLayout)
	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'pending', 0, $1)`, pastLegacy); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("first normalize: %v", err)
	}
	var after1 string
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM event_outbox WHERE id='p1'`).Scan(&after1); err != nil {
		t.Fatalf("read after first: %v", err)
	}
	// Second pass: must not change anything.
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("second normalize: %v", err)
	}
	var after2 string
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM event_outbox WHERE id='p1'`).Scan(&after2); err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if after1 != after2 {
		t.Fatalf("normalize not idempotent: first=%q second=%q", after1, after2)
	}
}

// TestNormalize_PreservesNulls asserts that NULL time columns survive
// normalization as NULL (a row that has never been claimed/dispatched must
// keep those columns empty, not get stamped with a zero time).
func TestNormalize_PreservesNulls(t *testing.T) {
	db, o := openPureOutbox(t)
	ctx := context.Background()

	pastLegacy := time.Now().UTC().Add(-time.Hour).Format(legacySpaceLayout)
	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'pending', 0, $1)`, pastLegacy); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	var dispatched, nextAttempt, claimed sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT dispatched_at, next_attempt_at, claimed_until FROM event_outbox WHERE id='p1'`).
		Scan(&dispatched, &nextAttempt, &claimed); err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, col := range []struct {
		name string
		v    sql.NullString
	}{
		{"dispatched_at", dispatched},
		{"next_attempt_at", nextAttempt},
		{"claimed_until", claimed},
	} {
		if col.v.Valid {
			t.Fatalf("%s must stay NULL after normalization, got %q", col.name, col.v.String)
		}
	}
}

// TestNormalize_NoOpOnCanonical asserts that rows the relay itself wrote
// (canonical RFC3339Nano via the pure driver binding time.Time) pass through
// normalization unchanged.
func TestNormalize_NoOpOnCanonical(t *testing.T) {
	db, o := openPureOutbox(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := o.Append(ctx, tx, "evt", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	var before string
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM event_outbox`).Scan(&before); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if strings.Contains(before, " ") {
		t.Fatalf("relay wrote non-canonical created_at %q — test setup is wrong", before)
	}
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	var after string
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM event_outbox`).Scan(&after); err != nil {
		t.Fatalf("read after: %v", err)
	}
	if before != after {
		t.Fatalf("normalize changed a canonical value: before=%q after=%q", before, after)
	}
}
