package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	stop     chan struct{}
	stopped  chan struct{}
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

// NewDBQueue constructs a DBQueue and ensures its backing table exists.
// Probes the dialect once via SELECT version(); falls back to SQLite.
// Panics if the table name contains unsafe characters.
func NewDBQueue(db *sql.DB, opts ...DBQueueOption) (*DBQueue, error) {
	q := &DBQueue{
		db:       db,
		table:    "queue_jobs",
		handlers: map[string]Handler{},
		workers:  1,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
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
		status        TEXT NOT NULL DEFAULT 'pending'
	)`, q.qt(), tsType, tsType)
	if _, err := q.db.Exec(stmt); err != nil {
		return err
	}
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

// RegisterHandler binds a job type to a handler. Required before Start.
func (q *DBQueue) RegisterHandler(jobType string, h Handler) {
	q.handlers[jobType] = h
}

// Enqueue inserts a job. Fills in ID/CreatedAt/MaxAttempts/ScheduledAt
// defaults when zero-valued so callers can pass {Type, Payload} only.
func (q *DBQueue) Enqueue(ctx context.Context, job Job) error {
	if job.ID == "" {
		job.ID = randomID()
	}
	now := time.Now().UTC()
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
	where, args := q.eligibleWhere(types, 1)
	// FOR UPDATE SKIP LOCKED is the canonical Postgres pattern: holds a
	// row lock for the surrounding UPDATE, lets concurrent workers skip
	// it instead of blocking.
	sqlStr := fmt.Sprintf(`UPDATE %s SET status='claimed', attempts = attempts + 1
		WHERE id = (
			SELECT id FROM %s
			WHERE %s
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, type, payload, priority, attempts, max_attempts, created_at, scheduled_at`,
		q.qt(), q.qt(), where)
	row := q.db.QueryRowContext(ctx, sqlStr, args...)
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
		fmt.Sprintf(`UPDATE %s SET status='claimed', attempts = attempts + 1 WHERE id = $1`, q.qt()),
		job.ID,
	); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	job.Attempts++
	return job, nil
}

// eligibleWhere builds the WHERE fragment for "pending and ready to run",
// optionally restricted to a set of job types. startIdx is the first $N to
// use so callers can prepend their own params.
func (q *DBQueue) eligibleWhere(types []string, startIdx int) (string, []any) {
	var args []any
	parts := []string{"status='pending'", "scheduled_at <= $" + itoa(startIdx)}
	args = append(args, time.Now().UTC())
	idx := startIdx + 1
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
func (q *DBQueue) Nack(ctx context.Context, jobID string) error {
	// One round-trip per nack: a CASE expression decides between requeue
	// and dead-letter based on the row's current attempts vs max_attempts.
	stmt := fmt.Sprintf(`UPDATE %s
		SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END
		WHERE id = $1`, q.qt())
	_, err := q.db.ExecContext(ctx, stmt, jobID)
	return err
}

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
// → Ack/Nack until Close.
func (q *DBQueue) Start(ctx context.Context) {
	remaining := q.workers
	go func() {
		defer close(q.stopped)
		done := make(chan struct{}, q.workers)
		for i := 0; i < q.workers; i++ {
			go func() { q.workerLoop(ctx); done <- struct{}{} }()
		}
		for remaining > 0 {
			<-done
			remaining--
		}
	}()
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
		job, err := q.Dequeue(ctx, keys(q.handlers)...)
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
		h, ok := q.handlers[job.Type]
		if !ok {
			// No handler — drop the row so it doesn't loop forever.
			_ = q.Ack(ctx, job.ID)
			continue
		}
		if err := h(ctx, job); err != nil {
			_ = q.Nack(ctx, job.ID)
			continue
		}
		_ = q.Ack(ctx, job.ID)
	}
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
