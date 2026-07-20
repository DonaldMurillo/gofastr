package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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
	logger  *slog.Logger

	handlers       map[string]Handler
	workers        int
	lease          time.Duration
	handlerTimeout time.Duration
	stop           chan struct{}
	stopped        chan struct{}

	// mu guards post-construction mutation of handlers, lease, and gate so
	// that RegisterHandler/SetLeaseTimeout/SetGate can race safely against
	// the worker loop's reads (workerLoop, eligibleWhere).
	mu sync.RWMutex

	// gate, when set, is checked in the worker loop after handlerFor. A
	// false return defers the job (release back to pending without consuming
	// a retry) rather than processing it. Framework code uses it to defer
	// jobs owned by a disabled module.
	gate func(jobType string) bool

	// laneWorkers maps a lane name to the number of dedicated worker
	// goroutines that only claim jobs in that lane. Populated by
	// WithLaneWorkers. Shared workers (from the workers field) claim any
	// lane. The empty lane ("") is the default lane.
	laneWorkers map[string]int

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

// WithDBLaneWorkers adds n dedicated worker goroutines that ONLY claim jobs
// whose Lane equals the given lane, on top of the shared workers from
// WithWorkers. This is the mechanism that prevents bulk backfills from
// starving urgent jobs: even when every shared worker is busy running a
// long-running bulk handler, the dedicated lane workers keep draining the
// reserved lane. Multiple calls for different lanes each add their own
// workers; multiple calls for the same lane sum. Priority ordering still
// selects among pending jobs within a worker's claim set. Panics if n <= 0
// or lane is "" (use WithWorkers for the shared/default pool). Named with
// the DB prefix to match WithDBHandlerTimeout / WithDBLogger (both backends
// share this package, so the MemoryQueue variant is WithLaneWorkers).
func WithDBLaneWorkers(lane string, n int) DBQueueOption {
	if n <= 0 {
		panic(fmt.Sprintf("queue.WithDBLaneWorkers: n must be > 0, got %d", n))
	}
	if lane == "" {
		panic("queue.WithDBLaneWorkers: lane must be non-empty (use WithWorkers for the default/shared pool)")
	}
	return func(q *DBQueue) {
		q.laneWorkers[lane] += n
	}
}

// WithWorkers sets the number of background worker goroutines started by
// Start(). Defaults to 1 when not set.
func WithWorkers(n int) DBQueueOption {
	return func(q *DBQueue) { q.workers = n }
}

// WithDBHandlerTimeout caps a single handler invocation's wall-clock
// budget. The job's context is cancelled at the deadline, so a
// black-holed dependency (an SMTP host that never responds, a hung HTTP
// call) can't wedge a worker forever — critical with the default single
// worker, where one stuck job stalls the entire queue. Zero (default)
// means no timeout; set it whenever handlers touch the network. (Named
// distinctly from the MemoryQueue's WithHandlerTimeout because both live
// in this package.)
func WithDBHandlerTimeout(d time.Duration) DBQueueOption {
	return func(q *DBQueue) { q.handlerTimeout = d }
}

// WithDBLogger sets the logger used for handler-failure (WARN) and
// dead-letter (ERROR) records emitted from the worker loop. Defaults to
// slog.Default(); passing nil restores the default. Without it a failing
// handler dead-letters silently — you lose the job and the reason.
func WithDBLogger(l *slog.Logger) DBQueueOption {
	return func(q *DBQueue) {
		if l == nil {
			l = slog.Default()
		}
		q.logger = l
	}
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
		db:          db,
		table:       "queue_jobs",
		logger:      slog.Default(),
		handlers:    map[string]Handler{},
		laneWorkers: map[string]int{},
		workers:     1,
		lease:       5 * time.Minute,
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
		now:         time.Now,
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
	// Canonicalize any space-separated (legacy mattn/go-sqlite3) timestamps
	// in the queue and scheduler tables before the first claim query runs.
	// SQLite-only no-op on Postgres; idempotent. See legacy_normalize.go.
	if err := q.normalizeLegacyTimestamps(context.Background()); err != nil {
		return nil, fmt.Errorf("normalize legacy timestamps: %w", err)
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
		occurrence_id TEXT NOT NULL DEFAULT '',
		type          TEXT NOT NULL,
		payload       TEXT,
		priority      INTEGER NOT NULL DEFAULT 0,
		lane          TEXT NOT NULL DEFAULT '',
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
	// Migrate the lane column onto pre-existing tables created before lane
	// isolation shipped. Postgres supports ADD COLUMN IF NOT EXISTS; SQLite
	// does not, so we attempt the ALTER and tolerate only the duplicate-column
	// error (matching on the message for both drivers).
	if err := q.migrateLaneColumn(); err != nil {
		return err
	}
	if err := q.migrateOccurrenceIDColumn(); err != nil {
		return err
	}
	// Index supports the dequeue ORDER BY and the WHERE filter together. The
	// shared-worker claim (any lane) is served by (status, scheduled_at,
	// priority); the lane-filtered claim used by dedicated lane workers is
	// served by (lane, status, scheduled_at, priority).
	for _, ix := range []struct{ name, cols string }{
		{q.table + "_dequeue_idx", "(status, scheduled_at, priority)"},
		{q.table + "_lane_idx", "(lane, status, scheduled_at, priority)"},
	} {
		safeIdx, err := query.SafeIdent(ix.name)
		if err != nil {
			return fmt.Errorf("queue: invalid index name %q: %w", ix.name, err)
		}
		idx := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s %s",
			query.QuoteIdent(safeIdx), q.qt(), ix.cols)
		if _, err := q.db.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

// migrateLaneColumn adds the lane column to a pre-existing table, tolerating
// the "column already exists" case so it is idempotent across versions.
func (q *DBQueue) migrateLaneColumn() error {
	if q.dialect == dialectPostgres {
		// Postgres supports IF NOT EXISTS directly.
		_, err := q.db.Exec(fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS lane TEXT NOT NULL DEFAULT ''", q.qt()))
		return err
	}
	// SQLite has no IF NOT EXISTS for ADD COLUMN: attempt and tolerate only
	// the duplicate-column error.
	_, err := q.db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN lane TEXT NOT NULL DEFAULT ''", q.qt()))
	if err != nil && isDuplicateColumnErr(err) {
		return nil
	}
	return err
}

// isDuplicateColumnErr reports whether err is the "column already exists"
// failure from ADD COLUMN, for either the SQLite (duplicate column name) or
// Postgres (already exists) driver.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
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

// SetGate installs a gate checked in the worker loop after a handler is
// resolved. When gate returns false the job is released back to pending
// (without consuming a retry attempt) and rescheduled slightly into the
// future to avoid a hot loop. Framework code uses it to defer jobs owned
// by a disabled module. Pass nil to clear.
func (q *DBQueue) SetGate(gate func(jobType string) bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gate = gate
}

// release returns a claimed job to pending without consuming a retry
// attempt (attempts is decremented to undo the Dequeue bump). The job's
// scheduled_at is pushed forward by gateDeferDelay so the worker doesn't
// immediately re-claim it in a tight loop.
func (q *DBQueue) release(ctx context.Context, jobID string) error {
	_, err := q.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='pending',
			attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
			scheduled_at = $1
			WHERE id = $2`, q.qt()),
		q.now().UTC().Add(gateDeferDelay), jobID)
	return err
}

// gateDeferDelay is how far into the future a gate-deferred job's
// scheduled_at is pushed. Short enough that re-enabling a module picks
// up deferred jobs within a second, long enough to break a tight
// dequeue→release→dequeue loop.
const gateDeferDelay = 100 * time.Millisecond

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

// eligibleTypes returns registered job types whose gate (if set) allows
// processing, so gated types are excluded before Dequeue and their rows are
// never claimed — eliminating the claim/release churn of grabbing a job only
// to release it. Both the handler map snapshot and the gate are read under
// the read lock so the result is consistent and race-free against concurrent
// SetGate/RegisterHandler. The captured gate function value is then invoked
// outside the lock (SetGate swaps the pointer, never mutates the old func).
func (q *DBQueue) eligibleTypes() []string {
	q.mu.RLock()
	types := keys(q.handlers)
	gate := q.gate
	q.mu.RUnlock()
	if gate == nil {
		return types
	}
	out := types[:0]
	for _, t := range types {
		if gate(t) {
			out = append(out, t)
		}
	}
	return out
}

// Enqueue inserts a job. Fills in ID/CreatedAt/MaxAttempts/ScheduledAt
// defaults when zero-valued so callers can pass {Type, Payload} only.
func (q *DBQueue) Enqueue(ctx context.Context, job Job) error {
	return q.enqueueWith(ctx, q.db, job)
}

// Dequeue claims the highest-priority eligible job in a single atomic step.
// Returns ErrNoJob when nothing is ready (no pending row whose scheduled_at
// has passed). Claims from ANY lane — dedicated lane workers use the
// internal dequeue with a lane filter instead.
func (q *DBQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	return q.dequeue(ctx, "", types)
}

// dequeue is the lane-aware claim used by both the public Dequeue (lane="",
// any lane) and the dedicated lane workers (lane != "", restricted to that
// lane). Sharing one code path keeps the two dialects' claim logic in sync.
func (q *DBQueue) dequeue(ctx context.Context, lane string, types []string) (Job, error) {
	switch q.dialect {
	case dialectPostgres:
		return q.dequeuePostgres(ctx, lane, types)
	default:
		return q.dequeueSQLite(ctx, lane, types)
	}
}
func (q *DBQueue) dequeuePostgres(ctx context.Context, lane string, types []string) (Job, error) {
	where, args := q.eligibleWhere(types, 2, lane)
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
		RETURNING id, occurrence_id, type, payload, priority, lane, attempts, max_attempts, created_at, scheduled_at`,
		q.qt(), q.qt(), where)
	row := q.db.QueryRowContext(ctx, sqlStr, claimArgs...)
	return scanJob(row)
}

func (q *DBQueue) dequeueSQLite(ctx context.Context, lane string, types []string) (Job, error) {
	// SQLite serialises writers at the file level, so a plain BEGIN+SELECT+
	// UPDATE+COMMIT is race-free even without SKIP LOCKED support.
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()

	where, args := q.eligibleWhere(types, 1, lane)
	pickSQL := fmt.Sprintf(`SELECT id, occurrence_id, type, payload, priority, lane, attempts, max_attempts, created_at, scheduled_at
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
// restricted to a set of job types and/or a single lane. startIdx is the
// first $N to use so callers can prepend their own params. A non-empty lane
// adds "lane = $N" so dedicated lane workers only claim their own lane.
//
// A row is eligible when it is 'pending', OR when it is 'claimed' but its
// lease has expired (the worker that claimed it crashed before Ack/Nack) and
// it still has retry attempts left. The lease-expiry clause is what makes
// in-flight work crash-safe: a claimed row is reclaimed instead of lost.
func (q *DBQueue) eligibleWhere(types []string, startIdx int, lane string) (string, []any) {
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

	if lane != "" {
		parts = append(parts, "lane = $"+itoa(idx))
		args = append(args, lane)
		idx++
	}

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
	base := fmt.Sprintf(`SELECT id, occurrence_id, type, payload, priority, lane, attempts,
		max_attempts, created_at, scheduled_at FROM %s`, q.qt())
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
		var createdAt, scheduledAt any
		if err := rows.Scan(&j.ID, &j.OccurrenceID, &j.Type, &payload, &j.Priority,
			&j.Lane, &j.Attempts, &j.MaxAttempts, &createdAt, &scheduledAt); err != nil {
			return nil, err
		}
		j.CreatedAt, err = queueTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("queue: decode job %q created_at: %w", j.ID, err)
		}
		j.ScheduledAt, err = queueTime(scheduledAt)
		if err != nil {
			return nil, fmt.Errorf("queue: decode job %q scheduled_at: %w", j.ID, err)
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

// Start launches the shared worker pool (q.workers goroutines that claim any
// lane) plus one goroutine per dedicated lane worker added via
// WithLaneWorkers. Each loops dequeue → handle → Ack/Nack until Close. A
// worker goroutine that dies for any reason (including a panic escaping the
// handler-recover guard) is respawned so the pool can never be permanently
// drained by a poison message.
func (q *DBQueue) Start(ctx context.Context) {
	// Count total workers so the done channel and join loop match.
	laneCount := 0
	for _, n := range q.laneWorkers {
		laneCount += n
	}
	total := q.workers + laneCount
	go func() {
		defer close(q.stopped)
		done := make(chan struct{}, total)
		// Shared workers claim any lane (lane="").
		for i := 0; i < q.workers; i++ {
			go q.superviseWorker(ctx, "", done)
		}
		// Dedicated lane workers only claim jobs in their lane. Iterate
		// lanes in sorted order so spawn order is deterministic (aids
		// reproducible test runs).
		for _, lane := range q.sortedLanes() {
			for i := 0; i < q.laneWorkers[lane]; i++ {
				go q.superviseWorker(ctx, lane, done)
			}
		}
		remaining := total
		for remaining > 0 {
			<-done
			remaining--
		}
	}()
}

// sortedLanes returns the lane names with dedicated workers in sorted order.
func (q *DBQueue) sortedLanes() []string {
	lanes := make([]string, 0, len(q.laneWorkers))
	for lane := range q.laneWorkers {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	return lanes
}

// superviseWorker runs workerLoop and respawns it if it ever returns
// abnormally (panic). It only reports done on a clean shutdown (stop/ctx),
// guaranteeing the pool size is preserved across poison-message panics. lane
// is "" for shared workers (claim any lane) or the dedicated lane name.
func (q *DBQueue) superviseWorker(ctx context.Context, lane string, done chan<- struct{}) {
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
		clean := q.runWorker(ctx, lane)
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
func (q *DBQueue) runWorker(ctx context.Context, lane string) (clean bool) {
	defer func() {
		if r := recover(); r != nil {
			clean = false
		}
	}()
	q.workerLoop(ctx, lane)
	return true
}

func (q *DBQueue) workerLoop(ctx context.Context, lane string) {
	// lane is "" for shared workers (claim any lane) or the dedicated lane
	// name (claim only jobs whose lane column matches).
	backoff := 100 * time.Millisecond
	// wait sleeps for backoff, or returns true if the queue is stopping or
	// the context was cancelled. Shared by the no-eligible-types and
	// ErrNoJob paths so both back off the same way.
	wait := func() bool {
		t := time.NewTimer(backoff)
		defer t.Stop()
		select {
		case <-q.stop:
			return true
		case <-ctx.Done():
			return true
		case <-t.C:
			return false
		}
	}
	for {
		select {
		case <-q.stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		// Filter gated types out of the eligible set BEFORE Dequeue so
		// gated jobs are never claimed — eliminating the claim/release
		// churn that would otherwise fire every ~100ms. When every
		// registered type is gated (or none are registered yet) there is
		// nothing to claim; back off and retry.
		types := q.eligibleTypes()
		if len(types) == 0 {
			if wait() {
				return
			}
			continue
		}
		job, err := q.dequeue(ctx, lane, types)
		if err != nil {
			// ErrNoJob is the steady state — sleep briefly and retry.
			if wait() {
				return
			}
			continue
		}
		h, ok := q.handlerFor(job.Type)
		if !ok {
			// No handler — drop the row so it doesn't loop forever.
			_ = q.Ack(ctx, job.ID)
			continue
		}
		// Gate race window: the gate may flip from allow to deny between
		// eligibleTypes() and this Dequeue claim. Re-check the gate under
		// the read lock (M4: every q.gate read is lock-protected) and, if
		// now gated, release the job back to pending without consuming a
		// retry. release() pushes scheduled_at forward so the worker
		// doesn't immediately re-claim it.
		q.mu.RLock()
		gate := q.gate
		q.mu.RUnlock()
		if gate != nil && !gate(job.Type) {
			_ = q.release(ctx, job.ID)
			continue
		}
		if err := q.runHandler(ctx, h, job); err != nil {
			// job.Attempts was bumped by Dequeue before the handler ran, so it
			// already equals the DB value Nack consults (attempts >=
			// max_attempts) — this is the exact terminal predicate, not an
			// off-by-one estimate.
			q.logger.Warn("queue: handler failed",
				"job_id", job.ID,
				"job_type", job.Type,
				"attempt", job.Attempts,
				"max_attempts", job.MaxAttempts,
				"err", err)
			if job.Attempts >= job.MaxAttempts {
				q.logger.Error("queue: job dead-lettered",
					"job_id", job.ID,
					"job_type", job.Type,
					"attempt", job.Attempts,
					"max_attempts", job.MaxAttempts,
					"err", err)
			}
			if err := q.Nack(ctx, job.ID); err != nil {
				q.logger.Warn("queue: nack failed", "job_id", job.ID, "err", err)
			}
			continue
		}
		if err := q.Ack(ctx, job.ID); err != nil {
			q.logger.Warn("queue: ack failed", "job_id", job.ID, "err", err)
		}
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
	q.mu.RLock()
	timeout := q.handlerTimeout
	q.mu.RUnlock()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
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
// canonical order used by Dequeue (lane included).
func scanJob(row interface {
	Scan(dest ...any) error
}) (Job, error) {
	var job Job
	var payload sql.NullString
	var lane sql.NullString
	var createdAt, scheduledAt any
	if err := row.Scan(
		&job.ID, &job.OccurrenceID, &job.Type, &payload,
		&job.Priority, &lane, &job.Attempts, &job.MaxAttempts,
		&createdAt, &scheduledAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return Job{}, ErrNoJob
		}
		return Job{}, err
	}
	var err error
	job.CreatedAt, err = queueTime(createdAt)
	if err != nil {
		return Job{}, fmt.Errorf("queue: decode job %q created_at: %w", job.ID, err)
	}
	job.ScheduledAt, err = queueTime(scheduledAt)
	if err != nil {
		return Job{}, fmt.Errorf("queue: decode job %q scheduled_at: %w", job.ID, err)
	}
	if payload.Valid && payload.String != "" {
		job.Payload = json.RawMessage(payload.String)
	}
	if lane.Valid {
		job.Lane = lane.String
	}
	return job, nil
}
