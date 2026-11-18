package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db/sqlbusy"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Delivery is a snapshot of one consumer's delivery of one row. Status is
// "pending", "dispatched", "dead" (attempts exhausted) or "abandoned" (no
// replica had the consumer's handler within the grace).
type Delivery struct {
	RowID         string
	Consumer      string
	Status        string
	Attempts      int
	LastError     string
	NextAttemptAt *time.Time
	DispatchedAt  *time.Time
}

// ListDeliveries returns a row's deliveries ordered by consumer, an empty
// slice when it has none.
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

// Replay sets every dead or abandoned delivery of a row back to pending with
// its attempts cleared, and reopens the row. A row with none is left alone.
func (o *Outbox) Replay(ctx context.Context, rowID string) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox: replay: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=0, last_error='', next_attempt_at=NULL, claimed_until=NULL, created_at=$2
			WHERE row_id=$1 AND status IN ('dead','abandoned')`, o.qd()),
		rowID, o.now().UTC())
	if err != nil {
		return fmt.Errorf("outbox: replay: reset deliveries: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='pending', dispatched_at=NULL WHERE id=$1 AND status='dispatched'`, o.qt()),
		rowID); err != nil {
		return fmt.Errorf("outbox: replay: reopen parent: %w", err)
	}
	if o.beforeReplayCommit != nil {
		o.beforeReplayCommit()
	}
	return tx.Commit()
}

// ReplayConsumer is Replay for one consumer's delivery.
func (o *Outbox) ReplayConsumer(ctx context.Context, rowID, consumer string) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("outbox: replay consumer: begin: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='pending', attempts=0, last_error='', next_attempt_at=NULL, claimed_until=NULL, created_at=$3
			WHERE row_id=$1 AND consumer=$2 AND status IN ('dead','abandoned')`, o.qd()),
		rowID, consumer, o.now().UTC())
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

func (o *Outbox) expandDeliveries(ctx context.Context) (int, error) {
	snap := o.declaredSnapshot()
	if len(snap) == 0 {
		return 0, nil
	}
	insertVerb, conflict := "INSERT INTO", ""
	if o.dialect == dialectPostgres {
		conflict = " ON CONFLICT (row_id, consumer) DO NOTHING"
	} else {
		insertVerb = "INSERT OR IGNORE INTO"
	}
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

func (o *Outbox) sweepParents(ctx context.Context) error {
	now := o.now().UTC()
	cutoff := now.Add(-o.handlerGrace)
	declaredTypes := make(map[string]bool)
	for _, c := range o.declaredSnapshot() {
		declaredTypes[c.eventType] = true
	}
	if len(declaredTypes) == 0 {
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
	args := []any{now, cutoff}
	var covered strings.Builder
	for _, c := range o.declaredPairs() {
		args = append(args, c.eventType, c.name)
		fmt.Fprintf(&covered, "\n\t\t\t  AND (p.type <> $%d OR EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id AND d.consumer = $%d))",
			len(args)-1, o.qd(), len(args))
	}
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s AS p SET status='dispatched', dispatched_at=$1
			WHERE p.status='pending'
			  AND p.created_at <= $2
			  AND EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id)
			  AND NOT EXISTS (SELECT 1 FROM %s d WHERE d.row_id = p.id AND d.status='pending')%s`,
			o.qt(), o.qd(), o.qd(), covered.String()),
		args...)
	return err
}

func joinPlaceholders(ph []string) string {
	var out strings.Builder
	for i, p := range ph {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(p)
	}
	return out.String()
}

func (o *Outbox) purgeExpired(ctx context.Context) error {
	if o.retention <= 0 {
		return nil
	}
	cutoff := o.now().UTC().Add(-o.retention)
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE row_id IN (
			SELECT p.id FROM %s p
			WHERE p.status='dispatched' AND p.created_at <= $1
			  AND NOT EXISTS (
			      SELECT 1 FROM %s d
			      WHERE d.row_id = p.id AND d.status <> 'dispatched'
			  ))`, o.qd(), o.qt(), o.qd()),
		cutoff); err != nil {
		return err
	}
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE status='dispatched' AND created_at <= $1
		  AND NOT EXISTS (
		      SELECT 1 FROM %s d
		      WHERE d.row_id = %s.id AND d.status <> 'dispatched'
		  )`, o.qt(), o.qd(), o.qt()),
		cutoff)
	return err
}

func (o *Outbox) completeParent(ctx context.Context, rowID, eventType string) {
	now := o.now().UTC()
	args := []any{now, rowID, now.Add(-o.handlerGrace)}
	var covered strings.Builder
	for _, c := range o.declaredFor(eventType) {
		args = append(args, c)
		fmt.Fprintf(&covered, "\n\t\t\t  AND EXISTS (SELECT 1 FROM %s WHERE row_id=$2 AND consumer=$%d)", o.qd(), len(args))
	}
	if _, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='dispatched', dispatched_at=$1
			WHERE id=$2 AND status='pending'
			  AND created_at <= $3
			  AND NOT EXISTS (
			      SELECT 1 FROM %s WHERE row_id=$2 AND status='pending'
			  )%s`, o.qt(), o.qd(), covered.String()),
		args...); err != nil {
		slog.Default().Error("outbox: complete parent failed; relay will retry the pending row",
			"row_id", rowID, "error", err)
	}
}

type claimedDelivery struct {
	RowID        string
	Consumer     string
	Type         string
	Payload      []byte
	Attempts     int
	CreatedAt    time.Time
	ClaimedUntil time.Time
}

func (o *Outbox) claimDeliveries(ctx context.Context) ([]claimedDelivery, error) {
	if err := o.deadLetterExpiredExhaustedClaims(ctx); err != nil {
		return nil, err
	}
	switch o.dialect {
	case dialectPostgres:
		return o.claimDeliveriesPostgres(ctx)
	default:
		return o.claimDeliveriesSQLite(ctx)
	}
}

func (o *Outbox) deadLetterExpiredExhaustedClaims(ctx context.Context) error {
	_, err := o.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET status='dead', last_error=$1, claimed_until=NULL, next_attempt_at=NULL
			WHERE status='pending' AND attempts >= $2 AND claimed_until IS NOT NULL AND claimed_until <= $3`,
			o.qd()),
		"claim budget exhausted without a settle (relay died between claim and settle)",
		o.maxAttempts, o.now().UTC())
	return err
}

func (o *Outbox) claimDeliveriesPostgres(ctx context.Context) ([]claimedDelivery, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	q := fmt.Sprintf(`WITH claimed AS (
		UPDATE %s SET claimed_until = $1, attempts = attempts + 1
		WHERE (row_id, consumer) IN (
			SELECT d.row_id, d.consumer FROM %s d
			JOIN %s p ON p.id = d.row_id
			WHERE d.status = 'pending'
			  AND (d.claimed_until IS NULL OR d.claimed_until <= $2)
			  AND (d.attempts < $4 OR d.claimed_until IS NULL)
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
	rows, err := o.db.QueryContext(ctx, q, claimUntil, now, o.batchSize, o.maxAttempts)
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
		d.ClaimedUntil = claimUntil
		out = append(out, d)
	}
	return out, rows.Err()
}

func (o *Outbox) claimDeliveriesSQLite(ctx context.Context) ([]claimedDelivery, error) {
	now := o.now().UTC()
	claimUntil := now.Add(o.lease)
	conn, err := o.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			//gofastr:allow(poisonedconn) SQLite BEGIN IMMEDIATE claim: since SQLite 3.7.11 ROLLBACK aborts pending reads instead of failing, so its error here means no transaction is left active, and retiring the conn would only drop a :memory: database
			if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
				slog.Default().Warn("outbox: rolling back an abandoned claim", "err", err)
			}
		}
	}()
	tx := sqliteConnTx{conn: conn}

	pick := fmt.Sprintf(`SELECT d.row_id, d.consumer, d.attempts + 1, p.type, p.payload, p.created_at
		FROM %s d JOIN %s p ON p.id = d.row_id
		WHERE d.status = 'pending'
		  AND (d.claimed_until IS NULL OR d.claimed_until <= $1)
		  AND (d.attempts < $2 OR d.claimed_until IS NULL)
		  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $1)
		ORDER BY p.created_at ASC LIMIT $3`, o.qd(), o.qt())
	rows, err := tx.QueryContext(ctx, pick, now, o.maxAttempts, o.batchSize)
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
		d.ClaimedUntil = claimUntil
		out = append(out, d)
		pairs = append(pairs, [2]string{d.RowID, d.Consumer})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		err := tx.Commit(ctx)
		committed = err == nil
		return nil, err
	}
	var clauses []string
	args := []any{claimUntil}
	for _, pr := range pairs {
		clauses = append(clauses, fmt.Sprintf("(row_id=$%d AND consumer=$%d)", len(args)+1, len(args)+2))
		args = append(args, pr[0], pr[1])
	}
	upd := fmt.Sprintf(`UPDATE %s SET claimed_until = $1, attempts = attempts + 1 WHERE %s`,
		o.qd(), strings.Join(clauses, " OR "))
	if _, err := tx.ExecContext(ctx, upd, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return out, nil
}

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
	d.CreatedAt, err = query.ParseDBTime(createdAt)
	if err != nil {
		return claimedDelivery{}, fmt.Errorf("outbox: decode delivery %q created_at: %w", d.Consumer, err)
	}
	if payload.Valid {
		d.Payload = []byte(payload.String)
	}
	return d, nil
}

func (o *Outbox) markDeliveryDispatched(ctx context.Context, d claimedDelivery) {
	err := sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
				SET status='dispatched', dispatched_at=$1, claimed_until=NULL, next_attempt_at=NULL, last_error=''
				WHERE row_id=$2 AND consumer=$3 AND status='pending' AND claimed_until=$4`, o.qd()),
			o.now().UTC(), d.RowID, d.Consumer, d.ClaimedUntil)
		return err
	})
	if err != nil {
		slog.Default().Error("outbox: mark delivery dispatched failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

func (o *Outbox) markDeliveryFailure(ctx context.Context, d claimedDelivery, cause error) {
	if d.Attempts >= o.maxAttempts {
		err := sqlbusy.Retry(ctx, func(ctx context.Context) error {
			_, err := o.db.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s
					SET status='dead', last_error=$1, claimed_until=NULL, next_attempt_at=NULL
					WHERE row_id=$2 AND consumer=$3 AND status='pending' AND claimed_until=$4`, o.qd()),
				truncateError(cause), d.RowID, d.Consumer, d.ClaimedUntil)
			return err
		})
		if err != nil {
			slog.Default().Error("outbox: mark delivery dead failed; lease recovery will retry",
				"row_id", d.RowID, "consumer", d.Consumer, "error", err)
		}
		return
	}
	next := o.now().UTC().Add(backoff.Exponential(o.backoffBase, o.backoffMax, d.Attempts))
	err := sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
					SET status='pending', last_error=$1, next_attempt_at=$2, claimed_until=NULL
					WHERE row_id=$3 AND consumer=$4 AND status='pending' AND claimed_until=$5`, o.qd()),
			truncateError(cause), next, d.RowID, d.Consumer, d.ClaimedUntil)
		return err
	})
	if err != nil {
		slog.Default().Error("outbox: requeue failed delivery failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

func (o *Outbox) requeueNoHandler(ctx context.Context, d claimedDelivery) {
	now := o.now().UTC()
	n, err := sqlbusy.Exec(ctx, func(ctx context.Context) (int64, error) {
		res, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
				SET status='abandoned', last_error='no consumer handler within grace', claimed_until=NULL, next_attempt_at=NULL
				WHERE row_id=$1 AND consumer=$2 AND status='pending' AND created_at <= $3 AND claimed_until=$4`, o.qd()),
			d.RowID, d.Consumer, now.Add(-o.handlerGrace), d.ClaimedUntil)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
	if err != nil {
		slog.Default().Error("outbox: abandon unhandled delivery failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
		return
	}
	if n > 0 {
		o.completeParent(ctx, d.RowID, d.Type)
		return
	}
	next := now.Add(o.backoffMax)
	err = sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := o.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
					SET status='pending',
					    attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
					    next_attempt_at=$1, claimed_until=NULL
					WHERE row_id=$2 AND consumer=$3 AND status='pending' AND claimed_until=$4`, o.qd()),
			next, d.RowID, d.Consumer, d.ClaimedUntil)
		return err
	})
	if err != nil {
		slog.Default().Error("outbox: requeue unhandled delivery failed; lease recovery will retry",
			"row_id", d.RowID, "consumer", d.Consumer, "error", err)
	}
}

func (o *Outbox) releaseUnstarted(ctx context.Context, ds []claimedDelivery) {
	for _, d := range ds {
		err := sqlbusy.Retry(ctx, func(ctx context.Context) error {
			_, err := o.db.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s
					SET attempts = attempts - 1, claimed_until=NULL
					WHERE row_id=$1 AND consumer=$2 AND claimed_until=$3`, o.qd()),
				d.RowID, d.Consumer, d.ClaimedUntil)
			return err
		})
		if err != nil {
			slog.Default().Error("outbox: release unstarted delivery failed; lease recovery will retry",
				"row_id", d.RowID, "consumer", d.Consumer, "error", err)
		}
	}
}

func invokeHandler(ctx context.Context, h event.EventHandler, ev event.Event, consumer string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("outbox: consumer %q for %q panicked: %v", consumer, ev.Type, r)
		}
	}()
	return h(ctx, ev)
}

type sqliteConnTx struct{ conn *sql.Conn }

func (t sqliteConnTx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.conn.QueryContext(ctx, q, args...)
}

func (t sqliteConnTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.conn.ExecContext(ctx, q, args...)
}

func (t sqliteConnTx) Commit(ctx context.Context) error {
	_, err := t.conn.ExecContext(ctx, "COMMIT")
	return err
}
