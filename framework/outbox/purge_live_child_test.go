package outbox

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// seedReplayRace appends a parent past the retention window with a dead
// delivery "replayed" and an in-flight sibling, both aged likewise.
func seedReplayRace(t *testing.T, db *sql.DB, o *Outbox) string {
	t.Helper()
	ctx := context.Background()
	tx, _ := db.BeginTx(ctx, nil)
	id, err := o.Append(ctx, tx, "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	past := o.now().UTC().Add(-2 * time.Hour)
	if _, err := db.Exec("UPDATE "+o.qt()+" SET created_at=$1 WHERE id=$2", past, id); err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct{ consumer, status string }{{"replayed", "dead"}, {"sibling", "pending"}} {
		if _, err := db.Exec("INSERT INTO "+o.qd()+" (row_id, consumer, status, attempts, last_error, created_at) VALUES ($1,$2,$3,$4,$5,$6)",
			id, d.consumer, d.status, 1, "boom", past); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// assertPurgeKeepsChild runs retention and asserts the pending "replayed"
// delivery and its parent survive it.
func assertPurgeKeepsChild(t *testing.T, o *Outbox, id string) {
	t.Helper()
	if err := o.purgeExpired(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if findRowMaybe(mustList(t, o, "", 0), id) == nil {
		t.Fatal("retention purged the parent of a pending delivery: the replayed event is lost")
	}
	if d := findDelivery(t, mustDeliveries(t, o, id), "replayed"); d.Status != "pending" {
		t.Fatalf("replayed delivery = %q, want pending", d.Status)
	}
}

// Under READ COMMITTED, completeParent's NOT EXISTS reads the snapshot from
// before Replay commits, so it completes the parent over the delivery
// Replay just reset. The relay still delivers that child; retention must
// not delete it first.
func TestPGReplayRaceKeepsLiveChild(t *testing.T) {
	db, o := pgOutbox(t, WithRetention(time.Hour))
	db.SetMaxOpenConns(4) // the seam's writes run beside Replay's open tx
	id := seedReplayRace(t, db, o)
	ctx := context.Background()

	o.beforeReplayCommit = func() {
		o.beforeReplayCommit = nil
		if _, err := db.Exec(`UPDATE `+o.qd()+` SET status='dispatched' WHERE row_id=$1 AND consumer='sibling'`, id); err != nil {
			t.Errorf("settle sibling: %v", err)
		}
		// Bounded: a Replay that locks the parent row would block this
		// call until Replay commits, which waits on this seam.
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		o.completeParent(cctx, id, "t")
	}
	if err := o.Replay(ctx, id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := findRow(t, mustList(t, o, "", 0), id).Status; got != "dispatched" {
		t.Fatalf("parent = %q after the race, want dispatched: the interleave did not reproduce", got)
	}
	assertPurgeKeepsChild(t, o, id)
}

// SQLite serialises writers, so the race cannot happen there; the state
// it leaves is seeded instead to pin the same purge guard.
func TestPurgeKeepsPendingChild(t *testing.T) {
	db, o := openOutbox(t, WithRetention(time.Hour))
	id := seedReplayRace(t, db, o)
	if _, err := db.Exec("UPDATE "+o.qt()+" SET status='dispatched' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE "+o.qd()+" SET status='pending', attempts=0 WHERE row_id=$1 AND consumer='replayed'", id); err != nil {
		t.Fatal(err)
	}
	assertPurgeKeepsChild(t, o, id)
}
