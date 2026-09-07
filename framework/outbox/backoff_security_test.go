package outbox

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// Pins (2026-09-06 adversarial pass, round 5):
// a delivery is eligible when pending, its lease expired, AND its backoff
// window elapsed (claimDeliveries doc comment, delivery.go:347-350); markDeliveryFailure
// and requeueNoHandler write next_attempt_at for exactly this spacing.
// Surfaces: framework/outbox/delivery.go::claimDeliveriesSQLite.
// Finding: the SQLite pick (delivery.go:479-484) lacks the
// `AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $)` predicate that
// claimDeliveriesPostgres enforces (delivery.go:416). Effect: a requeued delivery is
// re-claimed on the very next poll — the retry budget burns in milliseconds instead of
// the backoff cadence, and requeueNoHandler rows re-claim in a zero-delay hot loop for
// the whole handlerGrace.
// Fix: the SQLite pick carries the same next_attempt_at predicate the
// Postgres path has (GOFASTR1414 dialectdrift now pins the parity).

// TestOutboxRedSQLiteBackoffIgnored: after markDeliveryFailure requeues a delivery
// pending with a future next_attempt_at, an immediate claim must NOT hand it out —
// then, once the clock passes the window, it must be claimable again (guards against
// a fix that gates but never re-claims).
func TestOutboxRedSQLiteBackoffIgnored(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(5))
	ctx := context.Background()
	fake := time.Now().UTC()
	o.now = func() time.Time { return fake }
	o.backoffBase = time.Hour
	o.backoffMax = time.Hour

	id := redBackoffStage(t, db, o)
	first, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("setup broken: first claim: %v", err)
	}
	if len(first) != 1 || first[0].RowID != id || first[0].Attempts != 1 {
		t.Fatalf("setup broken: first claim returned %+v, want exactly the seeded delivery with post-increment attempts", first)
	}
	o.markDeliveryFailure(ctx, first[0], errors.New("boom"))

	// Sanity: the row requeued pending with a future next_attempt_at.
	d := findDelivery(t, mustDeliveries(t, o, id), "svc")
	if d.Status != "pending" || d.NextAttemptAt == nil || !d.NextAttemptAt.After(fake) {
		t.Fatalf("setup broken: after markDeliveryFailure got status=%q next=%v, want pending with a future next_attempt_at", d.Status, d.NextAttemptAt)
	}

	second, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	for _, c := range second {
		if c.RowID == id {
			t.Errorf("SECURITY: [outbox-sqlite-backoff-ignored] SQLite claim re-handed out delivery %s whose backoff window has not elapsed (next_attempt_at=%s; Postgres path enforces the predicate at delivery.go:416, SQLite pick at :479-484 does not) — the retry budget burns at poll speed instead of the backoff cadence", id, d.NextAttemptAt.Format(time.RFC3339))
		}
	}

	// Positive control: past the window the row must be claimable again.
	fake = fake.Add(90 * time.Minute)
	third, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("setup broken: third claim: %v", err)
	}
	found := false
	for _, c := range third {
		if c.RowID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("delivery %s stayed unclaimable past its elapsed backoff window — the guard must gate, not freeze", id)
	}
}

// TestOutboxRedRequeueBackoffIgnored: the no-handler requeue (requeueNoHandler's
// within-grace arm) requeues a delivery pending with next_attempt_at = now+backoffMax;
// an immediate claim must NOT hand it out, and it must return once the window elapses.
// Driven directly with the claimed snapshot, the same input relay.go's settle path
// passes it.
func TestOutboxRedRequeueBackoffIgnored(t *testing.T) {
	db, o := openOutbox(t, WithMaxAttempts(5), WithHandlerGrace(time.Hour))
	ctx := context.Background()
	fake := time.Now().UTC()
	o.now = func() time.Time { return fake }
	o.backoffMax = time.Hour

	id := redBackoffStage(t, db, o)
	first, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("setup broken: first claim: %v", err)
	}
	if len(first) != 1 || first[0].RowID != id || first[0].Attempts != 1 {
		t.Fatalf("setup broken: first claim returned %+v, want exactly the seeded delivery with post-increment attempts", first)
	}
	// No handler registered for the type on this replica and the delivery is
	// younger than the grace, so requeueNoHandler takes the requeue arm:
	// pending, attempt refunded, next_attempt_at = now+backoffMax.
	o.requeueNoHandler(ctx, first[0])

	d := findDelivery(t, mustDeliveries(t, o, id), "svc")
	if d.Status != "pending" || d.Attempts != 0 || d.NextAttemptAt == nil || !d.NextAttemptAt.After(fake) {
		t.Fatalf("setup broken: after requeueNoHandler got status=%q attempts=%d next=%v, want pending/0 with a future next_attempt_at", d.Status, d.Attempts, d.NextAttemptAt)
	}

	second, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	for _, c := range second {
		if c.RowID == id {
			t.Errorf("SECURITY: [outbox-sqlite-backoff-ignored] SQLite claim re-handed out no-handler delivery %s immediately after requeueNoHandler (next_attempt_at=%s) — requeueNoHandler rows re-claim in a zero-delay hot loop for the whole handlerGrace", id, d.NextAttemptAt.Format(time.RFC3339))
		}
	}

	// Positive control: past the window the row must be claimable again.
	fake = fake.Add(90 * time.Minute)
	third, err := o.claimDeliveries(ctx)
	if err != nil {
		t.Fatalf("setup broken: third claim: %v", err)
	}
	found := false
	for _, c := range third {
		if c.RowID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("delivery %s stayed unclaimable past its elapsed requeue backoff window — the guard must gate, not freeze", id)
	}
}

// redBackoffStage commits one parent event row and seeds its "svc" delivery as a
// fresh pending row (created_at = the outbox's overridden now), returning the
// parent id.
func redBackoffStage(t *testing.T, db *sql.DB, o *Outbox) string {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("setup broken: begin: %v", err)
	}
	id, err := o.Append(ctx, tx, "t", map[string]any{"k": 1})
	if err != nil {
		t.Fatalf("setup broken: append: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("setup broken: commit: %v", err)
	}
	insertDelivery(t, db, o, id, "svc", "pending", 0, "")
	return id
}
