package outbox

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Normalization's idempotency check must be driver-aware: on a host whose
// driver binds the space-separated format (mattn/go-sqlite3), rows in
// that format ARE canonical — rewriting them on every relay start is
// wasted write churn on the whole table. legacyTimeSets therefore
// compares against the layout the current driver binds, not a hardcoded
// RFC3339Nano.
func TestLegacyTimeSetsDriverAwareIdempotency(t *testing.T) {
	const mattnLayout = "2006-01-02 15:04:05.999999999-07:00"
	ref := time.Date(2026, 7, 20, 23, 59, 59, 0, time.UTC)

	// Pure-driver host: RFC3339Nano is canonical; space-format rewrites.
	sets, _, err := legacyTimeSets(time.RFC3339Nano, []timeCol{
		{"claimed_until", ref.Format(time.RFC3339Nano)},
	})
	if err != nil || len(sets) != 0 {
		t.Fatalf("RFC3339Nano row on pure driver: sets=%v err=%v, want none", sets, err)
	}
	sets, _, err = legacyTimeSets(time.RFC3339Nano, []timeCol{
		{"claimed_until", ref.Format(mattnLayout)},
	})
	if err != nil || len(sets) != 1 {
		t.Fatalf("space row on pure driver: sets=%v err=%v, want 1 rewrite", sets, err)
	}

	// mattn host: space format is canonical; RFC3339 rewrites.
	sets, _, err = legacyTimeSets(mattnLayout, []timeCol{
		{"claimed_until", ref.Format(mattnLayout)},
	})
	if err != nil || len(sets) != 0 {
		t.Fatalf("space row on mattn driver: sets=%v err=%v, want none (already canonical)", sets, err)
	}
	sets, _, err = legacyTimeSets(mattnLayout, []timeCol{
		{"claimed_until", ref.Format(time.RFC3339Nano)},
	})
	if err != nil || len(sets) != 1 {
		t.Fatalf("RFC3339 row on mattn driver: sets=%v err=%v, want 1 rewrite", sets, err)
	}
}

// probeBindLayout detects which text layout the connected driver uses for
// time.Time binds, and this pins it against the driver applications actually
// ship. That driver binds the space-separated SQLite layout, because
// sqlite/stdlib sets `_time_format=sqlite` on every DSN — mattn wrote that
// format, and battery/auth's parseTimeFlex and this package's own layout probe
// were written to read it.
//
// The assertion used to be RFC3339Nano, which was the in-house engine's
// binding and no shipped driver's. The probe's whole job is to answer "what
// does the connected driver do", so pinning it against an engine nothing runs
// was asserting the wrong half of the question. The RFC3339Nano branch is
// still covered by TestLegacyTimeSetsDriverAwareIdempotency, which exercises
// both layouts as pure logic.
func TestProbeBindLayoutOnShippedDriver(t *testing.T) {
	const sqliteLayout = "2006-01-02 15:04:05.999999999-07:00"
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	o, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	layout := o.probeBindLayout(t.Context())
	if layout != sqliteLayout {
		t.Fatalf("shipped driver probed layout %q, want the SQLite layout %q", layout, sqliteLayout)
	}
}
