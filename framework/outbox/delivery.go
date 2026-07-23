package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Delivery is a snapshot of one per-consumer delivery row. Status is one of
// "pending", "dispatched", "dead" (handler exhausted MaxAttempts), or
// "abandoned" (no consumer handler existed anywhere within the grace
// window — a removed consumer). Each (parent row, declared consumer) pair
// has exactly one delivery row; its lifecycle is independent of every
// sibling consumer's delivery for the same event (sibling isolation) — one
// consumer failing never blocks another.
type Delivery struct {
	RowID         string
	Consumer      string
	Status        string
	Attempts      int
	LastError     string
	NextAttemptAt *time.Time
	DispatchedAt  *time.Time
}

// ListDeliveries returns the per-consumer delivery rows for one parent
// row, ordered by consumer name. Returns an empty slice (not nil) for a
// parent with no deliveries (e.g. no declared consumer matched its type).
func (o *Outbox) ListDeliveries(ctx context.Context, rowID string) ([]Delivery, error) {
	rows, err := o.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT consumer, status, attempts, last_error, next_attempt_at, dispatched_at
			FROM %s WHERE row_id=$1 ORDER BY consumer`, o.qd()),
		rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		d.RowID = rowID
		var lastError sql.NullString
		var nextAttempt, dispatched any
		if err := rows.Scan(&d.Consumer, &d.Status, &d.Attempts, &lastError, &nextAttempt, &dispatched); err != nil {
			return nil, err
		}
		if lastError.Valid {
			d.LastError = lastError.String
		}
		d.NextAttemptAt, err = outboxTimePtr(nextAttempt)
		if err != nil {
			return nil, fmt.Errorf("outbox: decode delivery %q next_attempt_at: %w", d.Consumer, err)
		}
		d.DispatchedAt, err = outboxTimePtr(dispatched)
		if err != nil {
			return nil, fmt.Errorf("outbox: decode delivery %q dispatched_at: %w", d.Consumer, err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Replay resurrects ALL dead or abandoned deliveries of a parent row and
// reopens the parent so the relay re-completes it. Resurrected deliveries
// have attempts cleared and are scheduled immediately; the parent flips
// back to pending with dispatched_at cleared. Idempotent: only rows that
// have at least one dead/abandoned delivery are affected, so replaying a
// fully-dispatched or unknown row is a no-op. Sibling consumers already
// dispatched are untouched.
func (o *Outbox) Replay(ctx context.Context, rowID string) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox: replay: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=0, last_error='', next_attempt_at=NULL, claimed_until=NULL
			WHERE row_id=$1 AND status IN ('dead','abandoned')`, o.qd()),
		rowID)
	if err != nil {
		return fmt.Errorf("outbox: replay: reset deliveries: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// No dead/abandoned deliveries → nothing to reopen. Commit empty tx.
		return tx.Commit()
	}
	// Reopen the parent so the relay re-expands/re-completes. Guarded by
	// status='dispatched' so a still-pending parent isn't clobbered.
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='pending', dispatched_at=NULL WHERE id=$1 AND status='dispatched'`, o.qt()),
		rowID); err != nil {
		return fmt.Errorf("outbox: replay: reopen parent: %w", err)
	}
	return tx.Commit()
}

// ReplayConsumer resurrects a single consumer's dead or abandoned delivery
// for a row and reopens the parent. Like [Replay] but scoped to one
// consumer — use it to retry just the dead-lettered consumer after fixing
// its handler, or to re-deliver to a consumer that was removed and
// re-added (its delivery abandoned in the interim). Idempotent (no-op if
// the named delivery isn't dead/abandoned).
func (o *Outbox) ReplayConsumer(ctx context.Context, rowID, consumer string) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox: replay consumer: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=0, last_error='', next_attempt_at=NULL, claimed_until=NULL
			WHERE row_id=$1 AND consumer=$2 AND status IN ('dead','abandoned')`, o.qd()),
		rowID, consumer)
	if err != nil {
		return fmt.Errorf("outbox: replay consumer: reset delivery: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='pending', dispatched_at=NULL WHERE id=$1 AND status='dispatched'`, o.qt()),
		rowID); err != nil {
		return fmt.Errorf("outbox: replay consumer: reopen parent: %w", err)
	}
	return tx.Commit()
}

// expandDeliveries creates the missing pending delivery for each declared
// consumer on every pending parent row whose type matches the consumer's
// event type. Idempotent via a NOT EXISTS guard, so concurrent replicas or
// repeated pumps never create duplicate deliveries. Bounded to batchSize
// pending parents per consumer per pump so a large backlog doesn't turn
// one pump into a giant insert. Returns the number of deliveries created.
func (o *Outbox) expandDeliveries(ctx context.Context) (int, error) {
	snap := o.declaredSnapshot()
	if len(snap) == 0 {
		return 0, nil
	}
	// The NOT EXISTS guard is a read-time filter, not a concurrency guard:
	// two relays (different replicas) can both pass it and both INSERT the
	// same (row_id, consumer), which the PRIMARY KEY would reject with a
	// unique violation — aborting the whole statement and stalling the pump.
	// An idempotent upsert turns that benign race into a silent no-op.
	insertVerb, conflict := "INSERT INTO", ""
	if o.dialect == dialectPostgres {
		conflict = " ON CONFLICT (row_id, consumer) DO NOTHING"
	} else {
		insertVerb = "INSERT OR IGNORE INTO"
	}
	// Placeholders must first-appear in ascending order ($1,$2,…) — go-sqlite3
	// binds $N by first-appearance ordinal, not by the digit — so the delivery
	// created_at ($2) sits in the SELECT list before the WHERE ($3) and LIMIT
	// ($4). See the placeholder note in outbox.go.
	now := o.now().UTC()
	created := 0
	for _, c := range snap {
		res, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`%s %s (row_id, consumer, status, attempts, created_at, next_attempt_at, claimed_until)
				SELECT p.id, $1, 'pending', 0, $2, NULL, NULL
				FROM %s p
				WHERE p.status = 'pending'
				  AND p.type = $3
				  AND NOT EXISTS (
				      SELECT 1 FROM %s d WHERE d.row_id = p.id AND d.consumer = $1
				  )
				ORDER BY p.created_at ASC
				LIMIT $4%s`, insertVerb, o.qd(), o.qt(), o.qd(), conflict),
			c.name, now, c.eventType, o.batchSize)
		if err != nil {
			return created, err
		}
		if n, err := res.RowsAffected(); err == nil {
			created += int(n)
		}
	}
	return created, nil
}

// sweepParents settles pending parents that per-delivery completion won't
// reach on its own. Both paths are snapshot-free (no dependency on the
// live declared set), so they are safe across a rolling deploy that changes
// the consumer set:
//
//	(i)  Orphan drop — a pending parent older than handlerGrace, with NO
//	     delivery rows, whose event type has no declared consumer here.
//	     Both guards matter: the type guard means a still-consumed type is
//	     never dropped even when a >grace relay outage left a large backlog
//	     un-expanded (expand is batch-bounded and oldest-first, so it catches
//	     up over pumps); the age guard means a rolling deploy that adds a
//	     type's first consumer is safe (fresh parents are younger than the
//	     grace, so a lagging replica that lacks the consumer never drops them
//	     before an up-to-date replica expands them). Events staged more than
//	     handlerGrace before a type's first consumer is added are not
//	     back-delivered — a retention-style boundary.
//
//	(ii) Completion backstop — a pending parent older than the grace that
//	     HAS deliveries, all terminal (dispatched/dead/abandoned). Same age
//	     gate as completeParent (a young parent may still gain a delivery from
//	     a consumer being rolled out on another replica). This is also what
//	     actually completes the common case: completeParent no-ops while a
//	     parent is young, so this sweep finishes it once past the grace.
//	     EXISTS(delivery) keeps a never-expanded parent out of this path.
func (o *Outbox) sweepParents(ctx context.Context) error {
	now := o.now().UTC()
	cutoff := now.Add(-o.handlerGrace)
	// (i) orphan drop — old, never-expanded parents of an unconsumed type.
	declaredTypes := make(map[string]bool)
	for _, c := range o.declaredSnapshot() {
		declaredTypes[c.eventType] = true
	}
	if len(declaredTypes) == 0 {
		// No consumers at all: every old, delivery-less parent is orphan.
		if _, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s AS p SET status='dispatched', dispatched_at=$1
				WHERE p.status='pending' AND p.created_at <= $2
				  AND NOT EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id)`,
				o.qt(), o.qd()),
			now, cutoff); err != nil {
			return err
		}
	} else {
		ph := make([]string, 0, len(declaredTypes))
		args := []any{now, cutoff}
		i := 3
		for typ := range declaredTypes {
			ph = append(ph, fmt.Sprintf("$%d", i))
			args = append(args, typ)
			i++
		}
		if _, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s AS p SET status='dispatched', dispatched_at=$1
				WHERE p.status='pending' AND p.created_at <= $2
				  AND p.type NOT IN (%s)
				  AND NOT EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id)`,
				o.qt(), joinPlaceholders(ph), o.qd()),
			args...); err != nil {
			return err
		}
	}
	// (ii) completion backstop — old parent, all deliveries terminal.
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s AS p SET status='dispatched', dispatched_at=$1
			WHERE p.status='pending'
			  AND p.created_at <= $2
			  AND EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id)
			  AND NOT EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id AND d.status='pending')`,
			o.qt(), o.qd(), o.qd()),
		now, cutoff)
	return err
}

// joinPlaceholders joins placeholder tokens with ", " for an IN list.
func joinPlaceholders(ph []string) string {
	out := ""
	for i, p := range ph {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// purgeExpired deletes fully-settled (dispatched) parent rows and their
// deliveries once older than the retention window. No-op when retention is
// unset (0). Only dispatched parents are eligible, so a pending, dead, or
// abandoned delivery a consumer might still act on (or replay) is never
// deleted out from under it.
func (o *Outbox) purgeExpired(ctx context.Context) error {
	if o.retention <= 0 {
		return nil
	}
	cutoff := o.now().UTC().Add(-o.retention)
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE row_id IN (
			SELECT id FROM %s WHERE status='dispatched' AND created_at <= $1)`, o.qd(), o.qt()),
		cutoff); err != nil {
		return err
	}
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE status='dispatched' AND created_at <= $1`, o.qt()),
		cutoff)
	return err
}

// completeParent marks one parent dispatched iff it has no pending
// deliveries left (all dispatched/dead/abandoned) AND it is older than the
// handler grace. The age gate is essential and symmetric with abandonment:
// a consumer added on another replica has not yet had its delivery row
// created, so "no pending delivery" does NOT prove "fully delivered" for a
// young parent — completing it would let expand (WHERE status='pending')
// skip the parent forever and lose that consumer's event. Waiting the grace
// gives every replica time to expand its declared consumers. A parent may
// complete with some deliveries dead/abandoned; dead ones await Replay.
func (o *Outbox) completeParent(ctx context.Context, rowID string) {
	now := o.now().UTC()
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='dispatched', dispatched_at=$1
			WHERE id=$2 AND status='pending'
			  AND created_at <= $3
			  AND NOT EXISTS (
			      SELECT 1 FROM %s WHERE row_id=$2 AND status='pending'
			  )`, o.qt(), o.qd()),
		now, rowID, now.Add(-o.handlerGrace)); err != nil {
		slog.Default().Error("outbox: complete parent failed; relay will retry the pending row",
			"row_id", rowID, "error", err)
	}
}

// claimedDelivery carries what the relay needs to invoke a consumer's
// handler and settle its delivery row.
type claimedDelivery struct {
	RowID     string
	Consumer  string
	Type      string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}

// claimDeliveries claims up to batchSize eligible deliveries. A delivery
// is eligible when pending, its backoff window has elapsed, and it is not
// currently leased (or its lease has expired). Claiming sets
// claimed_until = now+lease at the delivery grain so a relay crash
// mid-batch releases only the unsettled deliveries (sibling isolation
// survives a crash too). Ordered by parent created_at for FIFO fairness.
func (o *Outbox) claimDeliveries(ctx context.Context) ([]claimedDelivery, error) {
	switch o.dialect {
	case dialectPostgres:
		return o.claimDeliveriesPostgres(ctx)
	default:
		return o.claimDeliveriesSQLite(ctx)
	}
}

// claimDeliveriesPostgres claims in one atomic step. FOR UPDATE SKIP
// LOCKED lets concurrent relays skip each other's deliveries instead of
// blocking. A CTE carries the RETURNING delivery rows into a join back to
// the parent for type/payload/created_at (UPDATE RETURNING can't surface
// columns from a joined table).
func (o *Outbox) claimDeliveriesPostgres(ctx context.Context) ([]claimedDelivery, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	q := fmt.Sprintf(`WITH claimed AS (
		UPDATE %s SET claimed_until = $1
		WHERE (row_id, consumer) IN (
			SELECT d.row_id, d.consumer FROM %s d
			JOIN %s p ON p.id = d.row_id
			WHERE d.status = 'pending'
			  AND (d.claimed_until IS NULL OR d.claimed_until <= $2)
			  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $2)
			ORDER BY p.created_at ASC
			LIMIT $3
			FOR UPDATE OF d SKIP LOCKED
		)
		RETURNING row_id, consumer, attempts
	)
	SELECT c.row_id, c.consumer, c.attempts, p.type, p.payload, p.created_at
	FROM claimed c
	JOIN %s p ON p.id = c.row_id
	ORDER BY p.created_at ASC`,
		o.qd(), o.qd(), o.qt(), o.qt())
	rows, err := o.db.QueryContext(ctx, q, claimUntil, now, o.batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claimedDelivery
	for rows.Next() {
		d, err := scanClaimedDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// claimDeliveriesSQLite claims via SELECT-then-UPDATE inside a tx. SQLite
// serialises writers (file-level lock under BEGIN), so the two-step is
// race-free without SKIP LOCKED. Mirrors the parent claim's SQLite path.
func (o *Outbox) claimDeliveriesSQLite(ctx context.Context) ([]claimedDelivery, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	pick := fmt.Sprintf(`SELECT d.row_id, d.consumer, d.attempts, p.type, p.payload, p.created_at
		FROM %s d JOIN %s p ON p.id = d.row_id
		WHERE d.status = 'pending'
		  AND (d.claimed_until IS NULL OR d.claimed_until <= $1)
		  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $1)
		ORDER BY p.created_at ASC LIMIT $2`, o.qd(), o.qt())
	rows, err := tx.QueryContext(ctx, pick, now, o.batchSize)
	if err != nil {
		return nil, err
	}
	var out []claimedDelivery
	var pairs [][2]string
	for rows.Next() {
		d, err := scanClaimedDelivery(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, d)
		pairs = append(pairs, [2]string{d.RowID, d.Consumer})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, tx.Commit()
	}
	// UPDATE the picked deliveries in the same tx so the claim is atomic.
	var clauses []string
	args := []any{claimUntil} // $1
	for _, pr := range pairs {
		clauses = append(clauses, fmt.Sprintf("(row_id=$%d AND consumer=$%d)", len(args)+1, len(args)+2))
		args = append(args, pr[0], pr[1])
	}
	upd := fmt.Sprintf(`UPDATE %s SET claimed_until = $1 WHERE %s`,
		o.qd(), strings.Join(clauses, " OR "))
	if _, err := tx.ExecContext(ctx, upd, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanClaimedDelivery materialises a claimedDelivery from a SELECT row in
// the canonical column order used by both claim queries.
func scanClaimedDelivery(row interface {
	Scan(dest ...any) error
}) (claimedDelivery, error) {
	var d claimedDelivery
	var payload sql.NullString
	var createdAt any
	if err := row.Scan(&d.RowID, &d.Consumer, &d.Attempts, &d.Type, &payload, &createdAt); err != nil {
		return claimedDelivery{}, err
	}
	var err error
	d.CreatedAt, err = outboxTime(createdAt)
	if err != nil {
		return claimedDelivery{}, fmt.Errorf("outbox: decode delivery %q created_at: %w", d.Consumer, err)
	}
	if payload.Valid {
		d.Payload = []byte(payload.String)
	}
	return d, nil
}

// markDeliveryDispatched settles a successful delivery. status<>'dispatched'
// keeps a settled delivery sticky: if this delivery raced a lease-expiry
// reclaim that already dispatched it, don't clobber that.
func (o *Outbox) markDeliveryDispatched(ctx context.Context, d claimedDelivery) {
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='dispatched', dispatched_at=$1, claimed_until=NULL, next_attempt_at=NULL, last_error=''
			WHERE row_id=$2 AND consumer=$3 AND status<>'dispatched'`, o.qd()),
		o.now().UTC(), d.RowID, d.Consumer); err != nil {
		slog.Default().Error("outbox: mark delivery dispatched failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

// markDeliveryFailure records a delivery failure on the delivery row. If
// this attempt exhausts MaxAttempts the delivery is marked dead; otherwise
// it returns to pending with an exponential backoff (next_attempt_at) so a
// flapping consumer can't burn through every attempt in a tight loop.
// status<>'dispatched' guards the lease-reclaim race.
func (o *Outbox) markDeliveryFailure(ctx context.Context, d claimedDelivery, cause error) {
	newAttempts := d.Attempts + 1
	if newAttempts >= o.maxAttempts {
		if _, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
				SET status='dead', attempts=$1, last_error=$2, claimed_until=NULL, next_attempt_at=NULL
				WHERE row_id=$3 AND consumer=$4 AND status<>'dispatched'`, o.qd()),
			newAttempts, truncateError(cause), d.RowID, d.Consumer); err != nil {
			slog.Default().Error("outbox: mark delivery dead failed; lease recovery will retry",
				"row_id", d.RowID, "consumer", d.Consumer, "error", err)
		}
		return
	}
	next := o.now().UTC().Add(o.backoffFor(newAttempts))
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=$1, last_error=$2, next_attempt_at=$3, claimed_until=NULL
			WHERE row_id=$4 AND consumer=$5 AND status<>'dispatched'`, o.qd()),
		newAttempts, truncateError(cause), next, d.RowID, d.Consumer); err != nil {
		slog.Default().Error("outbox: requeue failed delivery failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

// requeueNoHandler handles a delivery whose consumer has no handler on THIS
// replica. If the delivery has been unhandled since its own creation for
// longer than handlerGrace, no replica holds the consumer (a genuinely-
// removed consumer), so it is abandoned — settled terminal — and its parent
// completed. Otherwise it is requeued with a short backoff for an up-to-date
// replica to claim; it is never dead-lettered on attempts, because a replica
// mid-deploy may still hold the handler. Measuring the grace from the
// delivery's own created_at (not the event's) is what makes a freshly-added
// consumer's deliveries safe: they are young, so a lagging replica requeues
// rather than abandons them.
func (o *Outbox) requeueNoHandler(ctx context.Context, d claimedDelivery) {
	now := o.now().UTC()
	res, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='abandoned', last_error='no consumer handler within grace', claimed_until=NULL, next_attempt_at=NULL
			WHERE row_id=$1 AND consumer=$2 AND status='pending' AND created_at <= $3`, o.qd()),
		d.RowID, d.Consumer, now.Add(-o.handlerGrace))
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			o.completeParent(ctx, d.RowID)
			return
		}
	}
	// Requeue at the MAX backoff, not the base: a no-handler delivery may sit
	// for the whole grace window (a removal drain), and it sorts to the front
	// of the FIFO claim (oldest parents), so a base-interval requeue would
	// re-claim it every poll and both amplify writes and crowd out real work.
	// A coarse retry still picks it up within backoffMax once a handler lands.
	next := now.Add(o.backoffMax)
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', next_attempt_at=$1, claimed_until=NULL
			WHERE row_id=$2 AND consumer=$3 AND status='pending'`, o.qd()),
		next, d.RowID, d.Consumer); err != nil {
		slog.Default().Error("outbox: requeue unhandled delivery failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

// invokeHandler calls h with a deferred recover that converts a panic into
// a delivery error (rather than swallowing it, as the bus's emitSafe does).
// A panicking consumer is a retryable FAILURE — never a silent dispatched.
// Mirrors event.emitStrict's panic-surfacing contract.
func invokeHandler(ctx context.Context, h event.EventHandler, ev event.Event, consumer string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("outbox: consumer %q for %q panicked: %v", consumer, ev.Type, r)
		}
	}()
	return h(ctx, ev)
}
