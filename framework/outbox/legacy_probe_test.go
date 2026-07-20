package outbox

import (
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
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
// time.Time binds; the pure driver must probe as RFC3339Nano.
func TestProbeBindLayoutPureDriver(t *testing.T) {
	db, err := gosqlite.Open()
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
	if layout != time.RFC3339Nano {
		t.Fatalf("pure driver probed layout %q, want RFC3339Nano", layout)
	}
}
