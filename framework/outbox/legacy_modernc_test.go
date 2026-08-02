package outbox

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// openModerncOutbox opens the outbox on the driver the framework actually
// ships ("sqlite3" = modernc via sqlite/stdlib). The sibling tests in
// legacy_normalize_test.go use the repo's own pure engine, which no
// production path opens any more — so a defect that only appears on modernc
// slipped through them.
func openModerncOutbox(t *testing.T) (*sql.DB, *Outbox) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open modernc sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	o, err := New(db, WithHandlerGrace(0), WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	return db, o
}

// TestNormalizeReadsStoredTextNotDriverParsedTime pins the mechanism the
// idempotency skip depends on. The skip compares the stored text against the
// canonical text (legacyTimeSets: `c.raw.(string)`). modernc returns a
// time.Time for a DATETIME column, so without an explicit text cast that
// comparison can never match and EVERY row is rewritten on EVERY relay
// start — unbounded write churn against SQLite's single writer.
func TestNormalizeReadsStoredTextNotDriverParsedTime(t *testing.T) {
	db, o := openModerncOutbox(t)
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
	db, o := openModerncOutbox(t)
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
