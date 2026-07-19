package sqlite

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// CURRENT_TIMESTAMP / CURRENT_DATE / CURRENT_TIME as column DEFAULTs.
//
// These defaults are NON-constant: SQLite evaluates them PER INSERT, not at
// CREATE TABLE time. The OAuth account-links table declared by
// battery/auth/entity_oauth_links.go uses
//   created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
// and the adapter must accept an INSERT that omits created_at.
//
// Tests assert FORMAT (parses as the expected time layout), non-null, and
// RECENCY within a tolerance — they NEVER compare the clock value exactly.
// ============================================================================

const (
	timestampLayout  = "2006-01-02 15:04:05"
	dateLayout       = "2006-01-02"
	timeLayout       = "15:04:05"
	recencyTolerance = 5 * time.Second
)

// TestCurrentTimestampDefault_OmitColumn covers the OAuth-links shape:
// omitted created_at on a NOT NULL DEFAULT CURRENT_TIMESTAMP column must
// succeed and produce a non-null timestamp string in the right format,
// dated within a few seconds of now.
func TestCurrentTimestampDefault_OmitColumn(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE links (
		provider TEXT NOT NULL,
		provider_id TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (provider, provider_id)
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	before := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO links (provider, provider_id) VALUES ('google', 'g-1')`); err != nil {
		t.Fatalf("insert with omitted CURRENT_TIMESTAMP default failed: %v", err)
	}
	after := time.Now().UTC()

	var created string
	if err := db.QueryRow(`SELECT created_at FROM links WHERE provider = 'google'`).Scan(&created); err != nil {
		t.Fatalf("select: %v", err)
	}
	if created == "" {
		t.Fatal("created_at was NULL or empty")
	}
	parsed, err := time.Parse(timestampLayout, created)
	if err != nil {
		t.Fatalf("created_at %q does not parse as %q: %v", created, timestampLayout, err)
	}
	// Recency check with tolerance.
	if pt := parsed; pt.Before(before.Add(-recencyTolerance)) || pt.After(after.Add(recencyTolerance)) {
		t.Errorf("created_at %v outside [%v, %v] window", pt, before.Add(-recencyTolerance), after.Add(recencyTolerance))
	}
}

// TestCurrentDateDefault covers DEFAULT CURRENT_DATE on a TEXT column.
func TestCurrentDateDefault(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE d (
		id INTEGER PRIMARY KEY,
		d TEXT NOT NULL DEFAULT CURRENT_DATE
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	before := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO d (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	after := time.Now().UTC()

	var got string
	if err := db.QueryRow(`SELECT d FROM d WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := time.Parse(dateLayout, got); err != nil {
		t.Fatalf("d %q does not parse as %q: %v", got, dateLayout, err)
	}
	wantDay := before.Format(dateLayout)
	if got != wantDay && after.Format(dateLayout) != wantDay {
		// allow the day to be either side of midnight in the window
		if !(got == before.Format(dateLayout) || got == after.Format(dateLayout)) {
			t.Errorf("d: want today %q (or %q), got %q", before.Format(dateLayout), after.Format(dateLayout), got)
		}
	}
}

// TestCurrentTimeDefault covers DEFAULT CURRENT_TIME on a TEXT column.
func TestCurrentTimeDefault(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE ti (
		id INTEGER PRIMARY KEY,
		tm TEXT NOT NULL DEFAULT CURRENT_TIME
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	before := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO ti (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	after := time.Now().UTC()

	var got string
	if err := db.QueryRow(`SELECT tm FROM ti WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	parsed, err := time.Parse(timeLayout, got)
	if err != nil {
		t.Fatalf("tm %q does not parse as %q: %v", got, timeLayout, err)
	}
	now := time.Now().UTC()
	pt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC)
	if pt.Before(before.Add(-recencyTolerance)) || pt.After(after.Add(recencyTolerance)) {
		t.Errorf("tm %v outside [%v, %v] window", pt, before.Add(-recencyTolerance), after.Add(recencyTolerance))
	}
}

// TestCurrentTimestampReevaluatedPerInsert confirms the default is NOT
// cached: two inserts at different times get different values (within the
// resolution of the timestamp string).
func TestCurrentTimestampReevaluatedPerInsert(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE r (
		id INTEGER PRIMARY KEY,
		ts TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO r (id) VALUES (1)`); err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	// Sleep just over 1 second so the second-resolution timestamp string
	// is guaranteed to differ.
	time.Sleep(1100 * time.Millisecond)
	if _, err := db.Exec(`INSERT INTO r (id) VALUES (2)`); err != nil {
		t.Fatalf("insert 2: %v", err)
	}

	var ts1, ts2 string
	if err := db.QueryRow(`SELECT ts FROM r WHERE id = 1`).Scan(&ts1); err != nil {
		t.Fatalf("select 1: %v", err)
	}
	if err := db.QueryRow(`SELECT ts FROM r WHERE id = 2`).Scan(&ts2); err != nil {
		t.Fatalf("select 2: %v", err)
	}
	if ts1 == ts2 {
		t.Errorf("CURRENT_TIMESTAMP was not re-evaluated per insert: both rows got %q", ts1)
	}
}

// TestCurrentTimestampColumnPrecedence confirms a real column literally
// named "current_timestamp" still resolves to the COLUMN, not the keyword.
func TestCurrentTimestampColumnPrecedence(t *testing.T) {
	db := openTestDB(t)

	// Use a quoted identifier so the column name is literally the
	// keyword text; populate it with a known string and read it back.
	if _, err := db.Exec(`CREATE TABLE c (
		id INTEGER PRIMARY KEY,
		"current_timestamp" TEXT NOT NULL DEFAULT 'fixed'
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO c (id, "current_timestamp") VALUES (1, 'column-value')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got string
	if err := db.QueryRow(`SELECT "current_timestamp" FROM c WHERE id = 1`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "column-value" {
		t.Errorf(`"current_timestamp" column: want "column-value", got %q`, got)
	}

	// And the column-level DEFAULT ('fixed') should still apply when the
	// column is omitted — proving we did not fall through to the keyword.
	if _, err := db.Exec(`INSERT INTO c (id) VALUES (2)`); err != nil {
		t.Fatalf("insert with omitted column: %v", err)
	}
	var dflt string
	if err := db.QueryRow(`SELECT "current_timestamp" FROM c WHERE id = 2`).Scan(&dflt); err != nil {
		t.Fatalf("select default: %v", err)
	}
	if dflt != "fixed" {
		t.Errorf(`omitted column DEFAULT: want "fixed", got %q`, dflt)
	}
	// Sanity: the value must NOT look like a timestamp.
	if _, err := time.Parse(timestampLayout, dflt); err == nil {
		t.Errorf(`omitted column DEFAULT looked like a timestamp (%q) — keyword precedence is wrong`, dflt)
	}
}

// Compile-time assertion that we keep the sql import honest.
var _ = sql.NullString{}
var _ = strings.Contains
