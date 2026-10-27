package outbox

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// assertReplayRestartsGrace seeds an old event whose "svc" delivery was
// abandoned while the consumer was removed, replays it, and pumps a
// replica that does not hold the handler yet (mid rolling re-add). The
// replay must get a fresh grace: requeued for an up-to-date replica, not
// abandoned again on the spot.
func assertReplayRestartsGrace(t *testing.T, db *sql.DB, o *Outbox, replay func(*Outbox, string) error) {
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
	if _, err := db.Exec("UPDATE "+o.qt()+" SET status='dispatched', dispatched_at=$1, created_at=$1 WHERE id=$2", past, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+o.qd()+" (row_id, consumer, status, attempts, last_error, created_at) VALUES ($1,'svc','abandoned',1,'no consumer handler within grace',$2)",
		id, past); err != nil {
		t.Fatal(err)
	}

	if err := replay(o, id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	o.pump(ctx)

	if d := findDelivery(t, mustDeliveries(t, o, id), "svc"); d.Status != "pending" {
		t.Fatalf("replayed delivery = %q (%s) after a pump on a replica without the handler; want pending: the replay inherited the expired grace",
			d.Status, d.LastError)
	}
}

var replaySurfaces = []struct {
	name   string
	replay func(*Outbox, string) error
}{
	{"Replay", func(o *Outbox, id string) error { return o.Replay(context.Background(), id) }},
	{"ReplayConsumer", func(o *Outbox, id string) error { return o.ReplayConsumer(context.Background(), id, "svc") }},
}

func TestReplayRestartsHandlerGrace(t *testing.T) {
	for _, s := range replaySurfaces {
		t.Run(s.name, func(t *testing.T) {
			db, o := openOutbox(t, WithHandlerGrace(time.Hour))
			assertReplayRestartsGrace(t, db, o, s.replay)
		})
	}
}

func TestPGReplayRestartsHandlerGrace(t *testing.T) {
	for _, s := range replaySurfaces {
		t.Run(s.name, func(t *testing.T) {
			db, o := pgOutbox(t, WithHandlerGrace(time.Hour))
			assertReplayRestartsGrace(t, db, o, s.replay)
		})
	}
}
