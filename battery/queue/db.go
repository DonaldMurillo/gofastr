package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// DBQueue is a SQL-backed queue. Jobs persist in a single table; Dequeue
// claims a row atomically so multiple consumers can race safely.
//
// Postgres uses FOR UPDATE SKIP LOCKED — the canonical pattern for queue
// fan-out without distributed locks. SQLite uses a SERIALIZABLE-friendly
// SELECT-then-UPDATE inside a tx; the table-level lock SQLite takes on
// BEGIN IMMEDIATE serialises writers naturally.
type DBQueue struct {
	db      *sql.DB
	table   string
	dialect dbDialect

	handlers map[string]Handler
	workers  int
	lease    time.Duration
	stop     chan struct{}
	stopped  chan struct{}

	// mu guards post-construction mutation of handlers and lease so that
	// RegisterHandler/SetLeaseTimeout can race safely against the worker
	// loop's reads (workerLoop, eligibleWhere).
	mu sync.RWMutex

	// Retry backoff. When backoffBase > 0, a Nack with retries remaining
	// advances scheduled_at by backoffBase*2^(attempts-1), capped at
	// backoffMax. Zero base preserves the original "retry immediately"
	// behaviour.
	backoffBase time.Duration
	backoffMax  time.Duration

	// now is the clock used for claim timestamps, lease-expiry cutoffs, and
	// scheduled_at math. Defaults to time.Now; tests substitute a fake clock
	// so lease-reclaim behaviour can be asserted without wall-clock sleeps.
	now func() time.Time
}

type dbDialect int

const (
	dialectSQLite dbDialect = iota
	dialectPostgres
)

// DBQueueOption configures DBQueue construction.
type DBQueueOption func(*DBQueue)

// WithTable overrides the default "queue_jobs" table name.
func WithTable(name string) DBQueueOption {
	return func(q *DBQueue) { q.table = name }
}

// WithWorkers sets the number of background worker goroutines started by
// Start(). Defaults to 1 when not set.
func WithWorkers(n int) DBQueueOption {
	return func(q *DBQueue) { q.workers = n }
}

// WithLeaseTimeout sets how long a claimed-but-unacked job may stay in-flight
// before it is considered abandoned (the worker crashed/was killed) and
// becomes eligible for re-dequeue. Defaults to 5 minutes.
func WithLeaseTimeout(d time.Duration) DBQueueOption {
	return func(q *DBQueue) { q.lease = d }
}

// WithBackoff enables exponential retry backoff. On a Nack with retries
// remaining, scheduled_at is advanced by base*2^(attempts-1) — so the first
// retry waits ~base, the second ~2*base, and so on — capped at max. A
// non-positive base disables backoff (jobs retry immediately, the default).
// A non-positive max means uncapped. Mirrors the webhook battery's retry
// backoff so the two batteries behave consistently.
func WithBackoff(base, max time.Duration) DBQueueOption {
	return func(q *DBQueue) {
		q.backoffBase = base
		q.backoffMax = max
	}
}

// NewDBQueue constructs a DBQueue and ensures its backing table exists.
// Probes the dialect once via SELECT version(); falls back to SQLite.
// Panics if the table name contains unsafe characters.
func NewDBQueue(db *sql.DB, opts ...DBQueueOption) (*DBQueue, error) {
	q := &DBQueue{
		db:       db,
		table:    "queue_jobs",
		handlers: map[string]Handler{},
		workers:  1,
		lease:    5 * time.Minute,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(q)
	}
	// Validate table name once at construction time.
	query.MustIdent(q.table)
	q.dialect = detectDBDialect(db)
	if err := q.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure table: %w", err)
	}
	return q, nil
}

func detectDBDialect(db *sql.DB) dbDialect {
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		if strings.Contains(strings.ToLower(v), "postgresql") {
			return dialectPostgres
		}
	}
	return dialectSQLite
}

// qt returns the validated, quoted table name. Validated once at construction.
func (q *DBQueue) qt() string {
	return query.QuoteIdent(q.table)
}

func (q *DBQueue) ensureTable() error {
	tsType := "DATETIME"
	if q.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id            TEXT PRIMARY KEY,
		type          TEXT NOT NULL,
		payload       TEXT,
		priority      INTEGER NOT NULL DEFAULT 0,
		attempts      INTEGER NOT NULL DEFAULT 0,
		max_attempts  INTEGER NOT NULL DEFAULT 3,
		created_at    %s NOT NULL,
		scheduled_at  %s NOT NULL,
		status        TEXT NOT NULL DEFAULT 'pending',
		claimed_at    %s
	)`, q.qt(), tsType, tsType, tsType)
	if _, err := q.db.Exec(stmt); err != nil {
		return err
	}
	// Best-effort migration for pre-existing tables created before the
	// lease column existed. Ignore the error: re-running ADD COLUMN on a
	// table that already has it is the only expected failure here.
	_, _ = q.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN claimed_at %s", q.qt(), tsType))
	// Index supports the dequeue ORDER BY and the WHERE filter together.
	idxName := q.table + "_dequeue_idx"
	safeIdx, err := query.SafeIdent(idxName)
	if err != nil {
		return fmt.Errorf("queue: invalid index name %q: %w", idxName, err)
	}
	idx := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (status, scheduled_at, priority)",
		query.QuoteIdent(safeIdx), q.qt(),
	)
	_, err = q.db.Exec(idx)
	return err
}

// RegisterHandler binds a job type to a handler. Safe to call concurrently
// with a running worker loop.
func (q *DBQueue) RegisterHandler(jobType string, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[jobType] = h
}

// SetLeaseTimeout adjusts the in-flight lease duration after construction.
// See WithLeaseTimeout for semantics. Safe to call concurrently with a
// running worker loop.
func (q *DBQueue) SetLeaseTimeout(d time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lease = d
}

// leaseTimeout returns the current lease duration under the read lock.
func (q *DBQueue) leaseTimeout() time.Duration {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lease
}

// handlerFor returns the handler registered for jobType under the read lock.
func (q *DBQueue) handlerFor(jobType string) (Handler, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	h, ok := q.handlers[jobType]
	return h, ok
}

// handlerTypes returns a snapshot of registered job types under the read lock.
func (q *DBQueue) handlerTypes() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return keys(q.handlers)
}

// Enqueue inserts a job. Fills in ID/CreatedAt/MaxAttempts/ScheduledAt
// defaults when zero-valued so callers can pass {Type, Payload} only.
func (q *DBQueue) Enqueue(ctx context.Context, job Job) error {
	if job.ID == "" {
		job.ID = randomID()
	}
	now := q.now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	} else {
		job.CreatedAt = job.CreatedAt.UTC()
	}
	if job.ScheduledAt.IsZero() {
		job.ScheduledAt = now
	} else {
		job.ScheduledAt = job.ScheduledAt.UTC()
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	payload := string(job.Payload)
	if payload == "" {
		payload = "null"
	}
	_, err := q.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(id, type, payload, priority, attempts, max_attempts, created_at, scheduled_at, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending')`, q.qt()),
		job.ID, job.Type, payload, job.Priority, job.Attempts, job.MaxAttempts,
		job.CreatedAt, job.ScheduledAt,
	)
	return err
}

// Dequeue claims the highest-priority eligible job in a single atomic step.
// Returns ErrNoJob when nothing is ready (no pending row whose scheduled_at
// has passed).
func (q *DBQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	switch q.dialect {
	case dialectPostgres:
		return q.dequeuePostgres(ctx, types)
	default:
		return q.dequeueSQLite(ctx, types)
	}
}

func (q *DBQueue) dequeuePostgres(ctx context.Context, types []string) (Job, error) {
	where, args := q.eligibleWhere(types, 2)
	// $1 is the claim timestamp (claimed_at = now); eligibleWhere starts its
	// own placeholders at $2.
	claimArgs := append([]any{q.now().UTC()}, args...)
	// FOR UPDATE SKIP LOCKED is the canonical Postgres pattern: holds a
	// row lock for the surrounding UPDATE, lets concurrent workers skip
	// it instead of blocking.
	sqlStr := fmt.Sprintf(`UPDATE %s SET status='claimed', claimed_at=$1, attempts = attempts + 1
		WHERE id = (
			SELECT id FROM %s
			WHERE %s
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, payload, priority, attempts, max_attempts, created_at, scheduled_at`,
		q.qt(), q.qt(), where)
	row := q.db.QueryRowContext(ctx, sqlStr, claimArgs...)
	return scanJob(row)
}

func (q *DBQueue) dequeueSQLite(ctx context.Context, types []string) (Job, error) {
	// SQLite serialises writers at the file level, so a plain BEGIN+SELECT+
	// UPDATE+COMMIT is race-free even without SKIP LOCKED support.
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()

	where, args := q.eligibleWhere(types, 1)
	pickSQL := fmt.Sprintf(`SELECT id, type, payload, priority, attempts, max_attempts, created_at, scheduled_at
		FROM %s WHERE %s ORDER BY priority DESC, created_at ASC LIMIT 1`, q.qt(), where)
	row := tx.QueryRowContext(ctx, pickSQL, args...)
	job, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='claimed', claimed_at=$1, attempts = attempts + 1 WHERE id = $2`, q.qt()),
		q.now().UTC(), job.ID,
	); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	job.Attempts++
	return job, nil
}

// eligibleWhere builds the WHERE fragment for "ready to run", optionally
// restricted to a set of job types. startIdx is the first $N to use so
// callers can prepend their own params.
//
// A row is eligible when it is 'pending', OR when it is 'claimed' but its
// lease has expired (the worker that claimed it crashed before Ack/Nack) and
// it still has retry attempts left. The lease-expiry clause is what makes
// in-flight work crash-safe: a claimed row is reclaimed instead of lost.
func (q *DBQueue) eligibleWhere(types []string, startIdx int) (string, []any) {
	var args []any
	now := q.now().UTC()
	idx := startIdx
	// Placeholder numbers must increase in textual order: go-sqlite3 binds
	// positional args in order of appearance, ignoring the $N value. The
	// lease cutoff appears first (inside the status clause), then scheduled_at.
	// $idx: lease cutoff (claimed_at <= now-lease ⇒ abandoned)
	leaseIdx := idx
	args = append(args, now.Add(-q.leaseTimeout()))
	idx++
	// $idx: now (scheduled_at gate)
	schedIdx := idx
	args = append(args, now)
	idx++

	status := fmt.Sprintf(
		"(status='pending' OR (status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $%d AND attempts < max_attempts))",
		leaseIdx,
	)
	parts := []string{status, "scheduled_at <= $" + itoa(schedIdx)}

	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "$" + itoa(idx)
			args = append(args, t)
			idx++
		}
		parts = append(parts, "type IN ("+strings.Join(placeholders, ", ")+")")
	}
	return strings.Join(parts, " AND "), args
}

// Ack permanently removes the job — work is done, no replay needed.
func (q *DBQueue) Ack(ctx context.Context, jobID string) error {
	_, err := q.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, q.qt()), jobID)
	return err
}

// Nack returns a claimed job to the queue (status=pending) when it still
// has retry attempts left; otherwise marks it 'failed' for later inspection.
// When backoff is enabled (see WithBackoff), a requeued job's scheduled_at is
// pushed into the future so a flapping handler can't burn through every
// attempt in a tight loop.
func (q *DBQueue) Nack(ctx context.Context, jobID string) error {
	if q.backoffBase <= 0 {
		// No backoff: one round-trip. A CASE expression decides between
		// requeue and dead-letter based on attempts vs max_attempts.
		stmt := fmt.Sprintf(`UPDATE %s
			SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END
			WHERE id = $1`, q.qt())
		_, err := q.db.ExecContext(ctx, stmt, jobID)
		return err
	}

	// Backoff path: read attempts/max_attempts to decide requeue vs
	// dead-letter and to compute the next scheduled_at.
	var attempts, maxAttempts int
	row := q.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT attempts, max_attempts FROM %s WHERE id = $1", q.qt()), jobID)
	if err := row.Scan(&attempts, &maxAttempts); err != nil {
		if err == sql.ErrNoRows {
			return nil // already acked/removed; nothing to do
		}
		return err
	}
	if attempts >= maxAttempts {
		stmt := fmt.Sprintf("UPDATE %s SET status='failed' WHERE id = $1", q.qt())
		_, err := q.db.ExecContext(ctx, stmt, jobID)
		return err
	}
	next := q.now().UTC().Add(q.backoffFor(attempts))
	stmt := fmt.Sprintf("UPDATE %s SET status='pending', scheduled_at=$1 WHERE id = $2", q.qt())
	_, err := q.db.ExecContext(ctx, stmt, next, jobID)
	return err
}

// backoffFor returns the delay before the next retry given the number of
// attempts already made: base*2^(attempts-1), capped at backoffMax (when
// positive). attempts is expected to be >= 1 (Dequeue increments it before
// the handler runs).
func (q *DBQueue) backoffFor(attempts int) time.Duration {
	exp := attempts - 1
	if exp < 0 {
		exp = 0
	}
	d := q.backoffBase
	for i := 0; i < exp; i++ {
		// Stop doubling once we hit the cap or risk int64 overflow.
		if q.backoffMax > 0 && d >= q.backoffMax {
			return q.backoffMax
		}
		if d > (1<<62)/2 {
			// Next double would overflow; clamp to cap (or this value if uncapped).
			if q.backoffMax > 0 {
				return q.backoffMax
			}
			return d
		}
		d *= 2
	}
	if q.backoffMax > 0 && d > q.backoffMax {
		d = q.backoffMax
	}
	return d
}

// Replay implements [Replayable]: it resets a terminally-failed job to pending
// so a worker picks it up again — attempts cleared, scheduled immediately. The
// `AND status='failed'` clause makes it idempotent and safe: replaying an
// unknown, pending, running, or claimed job matches no row and is a no-op, so
// it can never double-run an in-flight job or resurrect a non-terminal one.
func (q *DBQueue) Replay(ctx context.Context, jobID string) error {
	stmt := fmt.Sprintf("UPDATE %s SET status='pending', attempts=0, scheduled_at=$1 WHERE id=$2 AND status='failed'", q.qt())
	_, err := q.db.ExecContext(ctx, stmt, q.now().UTC(), jobID)
	return err
}

// ListJobs implements [Browsable]. Returns up to limit jobs in the
// supplied status, newest-first. Empty status returns all jobs
// regardless of state. limit <= 0 defaults to 100.
func (q *DBQueue) ListJobs(ctx context.Context, status string, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	base := fmt.Sprintf(`SELECT id, type, payload, priority, attempts, max_attempts,
		created_at, scheduled_at FROM %s`, q.qt())
	args := []any{}
	if status != "" {
		base += " WHERE status = $1"
		args = append(args, status)
	}
	base += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)
	rows, err := q.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var payload string
		if err := rows.Scan(&j.ID, &j.Type, &payload, &j.Priority, &j.Attempts,
			&j.MaxAttempts, &j.CreatedAt, &j.ScheduledAt); err != nil {
			return nil, err
		}
		j.Payload = []byte(payload)
		out = append(out, j)
	}
	return out, rows.Err()
}

// Stats implements [Browsable]. Aggregates per-status counts over the
// whole table. Cheap: a single GROUP BY scan.
func (q *DBQueue) Stats(ctx context.Context) (JobStats, error) {
	rows, err := q.db.QueryContext(ctx,
		fmt.Sprintf("SELECT status, COUNT(*) FROM %s GROUP BY status", q.qt()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := JobStats{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

// Compile-time interface assertions — catching a missing implementation at build
// time rather than waiting for the test binary to be compiled and linked.
var (
	_ Queue      = (*DBQueue)(nil)
	_ Browsable  = (*DBQueue)(nil)
	_ Replayable = (*DBQueue)(nil)
)

// Close stops worker goroutines started by Start. Idempotent.
func (q *DBQueue) Close() error {
	select {
	case <-q.stop:
		return nil
	default:
		close(q.stop)
	}
	<-q.stopped
	return nil
}

// Start launches q.workers polling goroutines. Each loops Dequeue → handle
// → Ack/Nack until Close. A worker goroutine that dies for any reason
// (including a panic escaping the handler-recover guard) is respawned so the
// pool can never be permanently drained by a poison message.
func (q *DBQueue) Start(ctx context.Context) {
	remaining := q.workers
	go func() {
		defer close(q.stopped)
		done := make(chan struct{}, q.workers)
		for i := 0; i < q.workers; i++ {
			go q.superviseWorker(ctx, done)
		}
		for remaining > 0 {
			<-done
			remaining--
		}
	}()
}

// superviseWorker runs workerLoop and respawns it if it ever returns
// abnormally (panic). It only reports done on a clean shutdown (stop/ctx),
// guaranteeing the pool size is preserved across poison-message panics.
func (q *DBQueue) superviseWorker(ctx context.Context, done chan<- struct{}) {
	for {
		select {
		case <-q.stop:
			done <- struct{}{}
			return
		case <-ctx.Done():
			done <- struct{}{}
			return
		default:
		}
		clean := q.runWorker(ctx)
		if clean {
			done <- struct{}{}
			return
		}
		// Abnormal exit (panic that escaped the per-job guard): loop and
		// respawn rather than leaking a worker slot.
	}
}

// runWorker executes workerLoop, recovering any panic that escapes the
// per-job guard. Returns true if the loop exited cleanly (stop/ctx), false
// if it unwound via panic (so the supervisor respawns it).
func (q *DBQueue) runWorker(ctx context.Context) (clean bool) {
	defer func() {
		if r := recover(); r != nil {
			clean = false
		}
	}()
	q.workerLoop(ctx)
	return true
}

func (q *DBQueue) workerLoop(ctx context.Context) {
	backoff := 100 * time.Millisecond
	for {
		select {
		case <-q.stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		job, err := q.Dequeue(ctx, q.handlerTypes()...)
		if err != nil {
			// ErrNoJob is the steady state — sleep briefly and retry.
			t := time.NewTimer(backoff)
			select {
			case <-q.stop:
				t.Stop()
				return
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			continue
		}
		h, ok := q.handlerFor(job.Type)
		if !ok {
			// No handler — drop the row so it doesn't loop forever.
			_ = q.Ack(ctx, job.ID)
			continue
		}
		if err := q.runHandler(ctx, h, job); err != nil {
			_ = q.Nack(ctx, job.ID)
			continue
		}
		_ = q.Ack(ctx, job.ID)
	}
}

// runHandler invokes a job handler, converting a panic into an error so a
// poison-message job is nacked (retried/dead-lettered) instead of unwinding
// the worker goroutine and draining the pool / crashing the process.
func (q *DBQueue) runHandler(ctx context.Context, h Handler, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: handler for %q panicked: %v", job.Type, r)
		}
	}()
	return h(ctx, job)
}

func keys(m map[string]Handler) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// scanJob materialises a Job from a row that selects every column in the
// canonical order used by Dequeue.
func scanJob(row interface {
	Scan(dest ...any) error
}) (Job, error) {
	var job Job
	var payload sql.NullString
	if err := row.Scan(
		&job.ID, &job.Type, &payload, &job.Priority,
		&job.Attempts, &job.MaxAttempts, &job.CreatedAt, &job.ScheduledAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return Job{}, ErrNoJob
		}
		return Job{}, err
	}
	if payload.Valid && payload.String != "" {
		job.Payload = json.RawMessage(payload.String)
	}
	return job, nil
}
