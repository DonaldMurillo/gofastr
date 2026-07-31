package crud

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestAutoTimestampCarriesSubSecond pins that an AutoTimestamp value
// carries fractional-second precision. The old format
// (2006-01-02T15:04:05Z) was whole-second: two rows written in the same
// second got IDENTICAL created_at values, which makes the documented
// single-field cursor tie the common case on busy tables and stalls
// paging. Sub-second precision breaks those ties.
func TestAutoTimestampCarriesSubSecond(t *testing.T) {
	v, ok := generateFieldValue(schema.AutoTimestamp).(string)
	if !ok || v == "" {
		t.Fatalf("AutoTimestamp did not produce a string: %v", v)
	}
	if !strings.Contains(v, ".") {
		t.Errorf("AutoTimestamp %q lacks sub-second precision (no fractional component)", v)
	}
	// Must still be a strict RFC3339 timestamp (now with sub-second).
	if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
		t.Errorf("AutoTimestamp %q is not RFC3339Nano-parseable: %v", v, err)
	}
}

// TestAutoTimestampLexicographicOrder pins that the generated format is
// FIXED-WIDTH fractional (.000000, trailing zeros kept). A format that
// strips trailing zeros breaks the string-comparison ordering SQLite TEXT
// columns rely on: "…07.5Z" sorts AFTER "…07.5001Z" lexicographically but
// is chronologically earlier, which mis-orders both the cursor keyset
// WHERE and ORDER BY on the default sqlite backend.
func TestAutoTimestampLexicographicOrder(t *testing.T) {
	base := time.Date(2026, 7, 31, 15, 0, 7, 0, time.UTC)
	earlier := base.Add(500000 * time.Microsecond) // .500000
	later := base.Add(500100 * time.Microsecond)   // .500100
	const layout = "2006-01-02T15:04:05.000000Z07:00"
	a, b := earlier.UTC().Format(layout), later.UTC().Format(layout)
	if !(a < b) {
		t.Fatalf("fixed-width layout must keep string order chronological: %q !< %q", a, b)
	}
	// And the production path must use that fixed-width layout: EVERY
	// generated value carries exactly six fractional digits. Sampled,
	// because a zero-stripping layout (.999999) only betrays itself when
	// the instant's microseconds end in 0 — ~1 in 10 generations, near
	// certain across 200.
	for range 200 {
		v := generateFieldValue(schema.AutoTimestamp).(string)
		dot := strings.Index(v, ".")
		if dot == -1 || len(v)-dot-1 != 6+1 { // 6 digits + trailing 'Z'
			t.Fatalf("AutoTimestamp %q must have exactly 6 fractional digits (fixed width)", v)
		}
	}
}

// TestCreatedatRoundTripsSubSecond proves the generated timestamp survives
// a write→read round-trip on a SQLite TEXT column with its sub-second
// component intact. This is the column type the timestamp auto-generation
// path targets on SQLite; PG (TIMESTAMPTZ) round-trip is exercised by the
// testcontainers suite when Docker is available.
func TestCreatedatRoundTripsSubSecond(t *testing.T) {
	db := setupDB(t, `CREATE TABLE ts_rows (id TEXT PRIMARY KEY, created_at TEXT)`)

	v := generateFieldValue(schema.AutoTimestamp).(string)
	if _, err := db.Exec("INSERT INTO ts_rows (id, created_at) VALUES ($1, $2)", "r1", v); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var got string
	if err := db.QueryRow("SELECT created_at FROM ts_rows WHERE id = $1", "r1").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != v {
		t.Errorf("SQLite TEXT did not round-trip created_at: wrote %q read %q", v, got)
	}
	if !strings.Contains(got, ".") {
		t.Errorf("created_at lost sub-second precision in round-trip: %q", got)
	}
}
