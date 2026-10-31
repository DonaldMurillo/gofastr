package outbox

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The normalizer read every delivery, then wrote each back keyed on
// (row_id, consumer) alone. A relay that claimed the delivery in between
// had its fresh claimed_until replaced with the stale one read before, so
// another relay could claim it at once (2026-10-27 B10). Each write is
// guarded on the text it read.
func TestNormalizeKeepsDeliveryClaimedMeanwhile(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	o, err := New(db, WithHandlerGrace(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	layout := o.probeBindLayout(ctx)
	other := time.RFC3339Nano // whichever layout the driver does not bind
	if layout == other {
		other = legacyLayout
	}
	stale := time.Now().Add(-time.Hour).UTC()
	if _, err := db.Exec(`INSERT INTO event_outbox_delivery (row_id, consumer, status, attempts, created_at, claimed_until)
		VALUES ('row1', 'c1', 'pending', 0, $1, $1)`, stale.Format(other)); err != nil {
		t.Fatal(err)
	}
	var read string
	if err := db.QueryRow(`SELECT CAST(claimed_until AS TEXT) FROM event_outbox_delivery`).Scan(&read); err != nil {
		t.Fatal(err)
	}
	fixes, err := query.SQLiteTimeFixes(layout, []query.StoredTime{{Col: "created_at", Raw: read}, {Col: "claimed_until", Raw: read}})
	if err != nil || len(fixes) != 2 {
		t.Fatalf("fixes = %v (%v), want created_at and claimed_until", fixes, err)
	}

	fresh := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(`UPDATE event_outbox_delivery SET claimed_until=$1`, fresh); err != nil {
		t.Fatal(err) // a relay's claim lands between the read and the write
	}
	if err := o.applyTimeFixes(ctx, o.qd(), []string{"row_id", "consumer"}, []any{"row1", "c1"}, fixes); err != nil {
		t.Fatal(err)
	}

	var claimed any
	var created string
	if err := db.QueryRow(`SELECT claimed_until, CAST(created_at AS TEXT) FROM event_outbox_delivery`).Scan(&claimed, &created); err != nil {
		t.Fatal(err)
	}
	got, err := query.ParseDBTime(claimed)
	if err != nil || !got.Equal(fresh) {
		t.Fatalf("claimed_until = %v (%v), want the relay's fresh lease %v", got, err, fresh)
	}
	if want := stale.Format(layout); created != want {
		t.Fatalf("created_at = %q, want the untouched column rewritten to %q", created, want)
	}
}
