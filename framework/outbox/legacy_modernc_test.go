package outbox

import (
	"context"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The shared openOutbox helper (outbox_test.go) opens the outbox on the
// driver the framework actually ships ("sqlite3" = modernc via
// sqlite/stdlib), which is exactly what these pins need: a defect that
// only appears on modernc must not slip through tests again.

// TestNormalizeReadsStoredTextNotDriverParsedTime pins the mechanism the
// idempotency skip depends on. The skip compares the stored text against the
// canonical text (legacyTimeFixes reads it as a string). modernc returns a
// time.Time for a DATETIME column, so without an explicit text cast that
// comparison can never match and EVERY row is rewritten on EVERY relay
// start, unbounded write churn against SQLite's single writer.
func TestNormalizeReadsStoredTextNotDriverParsedTime(t *testing.T) {
	db, o := openOutbox(t, WithPollInterval(time.Millisecond))
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'pending', 0, $1)`, time.Now().UTC()); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	var raw any
	if err := db.QueryRowContext(ctx, o.parentTimeSelect()+` WHERE id='p1'`).Scan(new(string), &raw, new(any), new(any), new(any)); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, ok := raw.(string); !ok {
		t.Fatalf("normalizer reads created_at as %T; it must read stored text (string) "+
			"or the idempotency skip never fires and every row is rewritten on every relay start", raw)
	}
}

// TestNormalizeIsIdempotentOnModernc is the behavioural half: a second pass
// over already-canonical rows must not change anything.
func TestNormalizeIsIdempotentOnModernc(t *testing.T) {
	db, o := openOutbox(t, WithPollInterval(time.Millisecond))
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO event_outbox (id, type, payload, status, attempts, created_at)
		VALUES ('p1', 'evt', '{}', 'pending', 0, $1)`, time.Now().UTC()); err != nil {
		t.Fatalf("insert parent: %v", err)
	}
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("first normalize: %v", err)
	}
	var afterFirst string
	if err := db.QueryRowContext(ctx, `SELECT CAST(created_at AS TEXT) FROM event_outbox WHERE id='p1'`).Scan(&afterFirst); err != nil {
		t.Fatal(err)
	}
	if err := o.normalizeLegacyTimestamps(ctx); err != nil {
		t.Fatalf("second normalize: %v", err)
	}
	var afterSecond string
	if err := db.QueryRowContext(ctx, `SELECT CAST(created_at AS TEXT) FROM event_outbox WHERE id='p1'`).Scan(&afterSecond); err != nil {
		t.Fatal(err)
	}
	if afterFirst != afterSecond {
		t.Fatalf("normalization is not idempotent: %q then %q", afterFirst, afterSecond)
	}
}
