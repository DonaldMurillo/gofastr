package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Emitter is what the Relay publishes to. It is satisfied by
// *event.EventBus.
type Emitter interface {
	Emit(ctx context.Context, e event.Event) error
}

// strictEmitter is the optional panic-surfacing extension of Emitter. The
// framework's *event.EventBus implements it; the relay uses it when present
// so a panicking consumer is treated as a retryable delivery failure rather
// than being swallowed and falsely marked dispatched.
type strictEmitter interface {
	EmitStrict(ctx context.Context, e event.Event) error
}

// StartRelay launches the Relay goroutine. It claims batches of pending
// rows, publishes each to bus via the SYNCHRONOUS Emit (first handler
// error aborts and counts as a failed attempt), then marks the row
// dispatched. An Emit failure increments Attempts, records LastError,
// schedules an exponential backoff via next_attempt_at, and — once
// Attempts reaches MaxAttempts — marks the row dead.
//
// The loop runs until ctx is cancelled. The returned stop func blocks
// until the loop has fully exited, so callers can drain safely on
// shutdown.
func (o *Outbox) StartRelay(ctx context.Context, bus Emitter) (stop func()) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		o.relayLoop(ctx, bus, stopCh)
	}()
	return func() {
		select {
		case <-stopCh:
		default:
			close(stopCh)
		}
		<-doneCh
	}
}

// Nudge wakes the Relay immediately (non-blocking send on a cap-1
// channel). Callers invoke it right after commit so delivery latency is
// not bound to PollInterval. Extra nudges coalesce — only one wake is
// buffered regardless of how many arrive between pumps.
func (o *Outbox) Nudge() {
	select {
	case o.nudge <- struct{}{}:
	default:
	}
}

func (o *Outbox) relayLoop(ctx context.Context, bus Emitter, stop <-chan struct{}) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()
	for {
		// A full batch may mean a backlog — drain immediately instead
		// of waiting for the next tick. Still honour stop/ctx between
		// pumps: without this check a sustained backlog keeps n > 0
		// forever and stop() would block until the backlog drains.
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		default:
		}
		n := o.pump(ctx, bus)
		if n == 0 {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-o.nudge:
			case <-ticker.C:
			}
		}
	}
}

// pump claims one batch and settles each row synchronously. Returns the
// number of rows processed so the loop can decide whether to drain.
func (o *Outbox) pump(ctx context.Context, bus Emitter) int {
	rows, err := o.claimBatch(ctx)
	if err != nil {
		// The relay tolerates a claim failure (missing/renamed table under
		// WithoutEnsureTable, or a mid-run DB outage) by continuing to poll
		// rather than crashing the goroutine — but a silent no-op every
		// poll is exactly the "delivering nothing while looking alive"
		// trap the outbox exists to prevent. Log once on the transition
		// into failure (and once on recovery), not every poll, so an
		// outage is observable without flooding the log. pump runs only on
		// the single relay goroutine, so claimErrLogged needs no lock.
		if !o.claimErrLogged {
			slog.Default().Error("outbox: relay claim failed; delivery is stalled until this clears",
				"table", o.table, "err", err)
			o.claimErrLogged = true
		}
		return 0
	}
	if o.claimErrLogged {
		slog.Default().Info("outbox: relay claim recovered; delivery resumed", "table", o.table)
		o.claimErrLogged = false
	}
	if len(rows) == 0 {
		return 0
	}
	for _, r := range rows {
		if ctx.Err() != nil {
			// Shutdown mid-batch: claimed rows stay leased and are
			// recovered after the lease expires (at-least-once).
			break
		}
		o.processRow(ctx, bus, r)
	}
	return len(rows)
}

// claimedRow carries what the Relay needs to publish and settle a row.
type claimedRow struct {
	ID        string
	Type      string
	Payload   []byte
	Attempts  int
	CreatedAt time.Time
}

// processRow publishes one claimed row. Mark-dispatched happens AFTER a
// successful Emit, so a crash between the two re-delivers (consumers
// dedup by Event.ID). An Emit error records the failure and schedules a
// retry or marks the row dead.
func (o *Outbox) processRow(ctx context.Context, bus Emitter, r claimedRow) {
	var payload map[string]any
	if len(r.Payload) > 0 {
		if err := json.Unmarshal(r.Payload, &payload); err != nil {
			o.markFailure(ctx, r, fmt.Errorf("unmarshal payload: %w", err))
			return
		}
	}
	ev := event.Event{
		ID:        r.ID,
		Type:      r.Type,
		Data:      payload,
		Timestamp: r.CreatedAt,
	}
	// Prefer a panic-surfacing emit: the bus's plain Emit swallows a
	// panicking subscriber (to protect the tx of hook callers), which for
	// the outbox would falsely mark a lost event as dispatched. EmitStrict
	// turns that panic into a delivery error so the row is retried/dead-
	// lettered instead. Fall back to Emit for emitters that don't implement
	// it.
	emit := bus.Emit
	if se, ok := bus.(strictEmitter); ok {
		emit = se.EmitStrict
	}
	if err := emit(ctx, ev); err != nil {
		o.markFailure(ctx, r, err)
		return
	}
	o.markDispatched(ctx, r.ID)
}

func (o *Outbox) markDispatched(ctx context.Context, id string) {
	// status<>'dispatched' keeps a settled row sticky: if this delivery
	// raced a lease-expiry reclaim that already dispatched the row, don't
	// clobber it.
	_, _ = o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='dispatched', dispatched_at=$1, claimed_until=NULL, next_attempt_at=NULL
			WHERE id=$2 AND status<>'dispatched'`, o.qt()),
		o.now().UTC(), id)
}

// markFailure records the failure. If this attempt exhausts MaxAttempts
// the row is marked dead; otherwise it returns to pending with an
// exponential backoff (next_attempt_at) so a flapping handler can't
// burn through every attempt in a tight loop.
func (o *Outbox) markFailure(ctx context.Context, r claimedRow, cause error) {
	newAttempts := r.Attempts + 1
	// status<>'dispatched' guards the lease-reclaim race: a slow delivery
	// that errors after another relay already dispatched the row must not
	// resurrect a settled row back to pending/dead.
	if newAttempts >= o.maxAttempts {
		_, _ = o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
				SET status='dead', attempts=$1, last_error=$2, claimed_until=NULL
				WHERE id=$3 AND status<>'dispatched'`, o.qt()),
			newAttempts, truncateError(cause), r.ID)
		return
	}
	next := o.now().UTC().Add(o.backoffFor(newAttempts))
	_, _ = o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=$1, last_error=$2, next_attempt_at=$3, claimed_until=NULL
			WHERE id=$4 AND status<>'dispatched'`, o.qt()),
		newAttempts, truncateError(cause), next, r.ID)
}

// claimBatch atomically claims up to batchSize pending rows. A row is
// eligible when it is pending, its backoff window has elapsed, and it is
// not currently leased (or its lease has expired). Claiming sets
// claimed_until = now+lease so a Relay crash mid-batch releases the rows
// after the lease expires.
func (o *Outbox) claimBatch(ctx context.Context) ([]claimedRow, error) {
	switch o.dialect {
	case dialectPostgres:
		return o.claimPostgres(ctx)
	default:
		return o.claimSQLite(ctx)
	}
}

// claimPostgres claims in one atomic step. FOR UPDATE SKIP LOCKED lets
// concurrent Relays skip each other's rows instead of blocking — the
// canonical Postgres fan-out pattern (mirrors battery/queue).
func (o *Outbox) claimPostgres(ctx context.Context) ([]claimedRow, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	q := fmt.Sprintf(`UPDATE %s SET claimed_until = $1
		WHERE id IN (
			SELECT id FROM %s
			WHERE status = 'pending'
			  AND (claimed_until IS NULL OR claimed_until <= $2)
			  AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
			ORDER BY created_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, payload, attempts, created_at`,
		o.qt(), o.qt())
	rows, err := o.db.QueryContext(ctx, q, claimUntil, now, o.batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claimedRow
	for rows.Next() {
		r, err := scanClaimed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// claimSQLite claims via SELECT-then-UPDATE inside a transaction. SQLite
// serialises writers (file-level lock under BEGIN), so the two-step is
// race-free without SKIP LOCKED. Mirrors battery/queue's dequeueSQLite.
func (o *Outbox) claimSQLite(ctx context.Context) ([]claimedRow, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	pick := fmt.Sprintf(`SELECT id, type, payload, attempts, created_at FROM %s
		WHERE status = 'pending'
		  AND (claimed_until IS NULL OR claimed_until <= $1)
		  AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
		ORDER BY created_at ASC LIMIT $2`, o.qt())
	rows, err := tx.QueryContext(ctx, pick, now, o.batchSize)
	if err != nil {
		return nil, err
	}
	var out []claimedRow
	var ids []string
	for rows.Next() {
		r, err := scanClaimed(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		// Nothing eligible — commit the empty tx cleanly.
		return nil, tx.Commit()
	}

	// UPDATE the picked rows in the same tx so the claim is atomic.
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, claimUntil) // $1
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2) // $2, $3, …
		args = append(args, id)
	}
	upd := fmt.Sprintf(`UPDATE %s SET claimed_until = $1 WHERE id IN (%s)`,
		o.qt(), strings.Join(placeholders, ", "))
	if _, err := tx.ExecContext(ctx, upd, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// scanClaimed materialises a claimedRow from a RETURNING/SELECT row in
// the canonical column order used by the claim queries.
func scanClaimed(row interface {
	Scan(dest ...any) error
}) (claimedRow, error) {
	var r claimedRow
	var payload sql.NullString
	if err := row.Scan(&r.ID, &r.Type, &payload, &r.Attempts, &r.CreatedAt); err != nil {
		return claimedRow{}, err
	}
	if payload.Valid {
		r.Payload = []byte(payload.String)
	}
	return r, nil
}
