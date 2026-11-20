package queue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/backoff"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/db/sqlbusy"
)

// DBQueue is a SQL-backed queue on SQLite or Postgres, with a worker pool,
// leases, lanes, retries and replay.
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

	started bool

	mu sync.RWMutex

	gate func(jobType string) bool

	laneWorkers map[string]int

	backoffBase   time.Duration
	doneRetention time.Duration
	lastPrune     time.Time
	backoffMax    time.Duration

	retrySchedule []time.Duration
	scheduleSet   bool
	jitter        float64
	jitterRand    func() float64

	claimFailing atomic.Bool

	now func() time.Time

	afterClaim func(Job)
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

// WithDBLaneWorkers adds n workers that claim only jobs of lane, on top of the
// shared workers.
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

// WithWorkers sets the number of shared worker goroutines Start launches.
func WithWorkers(n int) DBQueueOption {
	return func(q *DBQueue) {
		if n >= 0 {
			q.workers = n
		}
	}
}

// WithDBHandlerTimeout caps one handler invocation's wall-clock budget.
func WithDBHandlerTimeout(d time.Duration) DBQueueOption {
	return func(q *DBQueue) { q.handlerTimeout = d }
}

// WithDBLogger sets the logger for the worker loop's handler-failure and
// dead-letter records.
func WithDBLogger(l *slog.Logger) DBQueueOption {
	return func(q *DBQueue) {
		if l == nil {
			l = slog.Default()
		}
		q.logger = l
	}
}

// WithLeaseTimeout sets how long a claimed, unacked job stays in flight before
// another worker may claim it.
func WithLeaseTimeout(d time.Duration) DBQueueOption {
	return func(q *DBQueue) {
		if d > 0 {
			q.lease = d
		}
	}
}

// WithBackoff enables exponential retry backoff from base, capped at max.
func WithBackoff(base, max time.Duration) DBQueueOption {
	return func(q *DBQueue) {
		q.backoffBase = base
		q.backoffMax = max
	}
}

// WithRetrySchedule sets the wait after each failed attempt from a fixed list
// instead of doubling, the last value reused.
func WithRetrySchedule(steps ...time.Duration) DBQueueOption {
	return func(q *DBQueue) {
		q.retrySchedule = append([]time.Duration(nil), steps...)
		q.scheduleSet = true
	}
}

// WithRetryJitter spreads each retry delay across plus or minus fraction of
// itself.
func WithRetryJitter(fraction float64) DBQueueOption {
	return func(q *DBQueue) { q.jitter = fraction }
}

func (q *DBQueue) validateRetryPolicy() error {
	if q.scheduleSet {
		if len(q.retrySchedule) == 0 {
			return errors.New("queue: WithRetrySchedule needs at least one step")
		}
		for i, d := range q.retrySchedule {
			if d < 0 {
				return fmt.Errorf("queue: WithRetrySchedule step %d is negative (%s)", i, d)
			}
		}
		if q.backoffBase > 0 {
			return errors.New("queue: WithRetrySchedule and WithBackoff both set the retry delay; pick one")
		}
	}
	if math.IsNaN(q.jitter) || q.jitter < 0 || q.jitter > 1 {
		return fmt.Errorf("queue: WithRetryJitter fraction must be in [0, 1] (got %v)", q.jitter)
	}
	return nil
}

func (q *DBQueue) retryDelay(attempts int) time.Duration {
	var d time.Duration
	switch {
	case len(q.retrySchedule) > 0:
		d = backoff.At(q.retrySchedule, attempts)
	case q.backoffBase > 0:
		d = backoff.Exponential(q.backoffBase, q.backoffMax, attempts)
	default:
		return 0
	}
	if q.jitter > 0 {
		//gofastr:allow(recovercallback) package-test seam; production holds math/rand/v2.Float64, which cannot panic
		d = backoff.Jitter(d, q.jitter, q.jitterRand())
	}
	return d
}

// WithDoneRetention keeps an acked job as status "done" for d instead of
// deleting it.
func WithDoneRetention(d time.Duration) DBQueueOption {
	return func(q *DBQueue) { q.doneRetention = max(d, 0) }
}

const maxLastError = 500

const donePruneEvery = time.Minute

// NewDBQueue constructs a DBQueue and ensures its backing table exists.
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
		jitterRand:  rand.Float64,
	}
	for _, opt := range opts {
		opt(q)
	}
	if err := leaseCoversBudget(q.lease, q.handlerTimeout); err != nil {
		return nil, err
	}
	if err := q.validateRetryPolicy(); err != nil {
		return nil, err
	}
	query.MustIdent(q.table)
	q.dialect = detectDBDialect(db)
	if err := q.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure table: %w", err)
	}
	if err := q.normalizeLegacyTimestamps(context.Background()); err != nil {
		return nil, fmt.Errorf("normalize legacy timestamps: %w", err)
	}
	registerJobsTable(q.table)
	return q, nil
}

func detectDBDialect(db *sql.DB) dbDialect {
	if query.IsPostgres(db) {
		return dialectPostgres
	}
	return dialectSQLite
}

func (q *DBQueue) qt() string {
	return query.QuoteFolded(q.table)
}

func (q *DBQueue) ensureTable() error {
	tsType := "DATETIME"
	if q.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
		if err := query.RefuseCaseKeptTables(context.Background(), q.db, q.table,
			q.schedulerName("_scheduler_schedules"), q.schedulerName("_scheduler_occurrences"),
			q.schedulerName("_scheduler_leases")); err != nil {
			return fmt.Errorf("queue: %w", err)
		}
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id            TEXT PRIMARY KEY NOT NULL,
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
		claimed_at    %s,
		claim_token   TEXT NOT NULL DEFAULT '',
		user_id       TEXT NOT NULL DEFAULT '',
		unique_key    TEXT NOT NULL DEFAULT '',
		updated_at    %s,
		last_error    TEXT NOT NULL DEFAULT '',
		max_age_ns    %s NOT NULL DEFAULT 0,
		sequence_key  TEXT NOT NULL DEFAULT ''
	)`, q.qt(), tsType, tsType, tsType, tsType, q.intType())
	if _, err := q.db.Exec(stmt); err != nil {
		return err
	}
	_, _ = q.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN claimed_at %s", q.qt(), tsType))
	_, _ = q.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN updated_at %s", q.qt(), tsType))
	if err := q.addTextColumn("lane"); err != nil {
		return err
	}
	if err := q.addTextColumn("occurrence_id"); err != nil {
		return err
	}
	if err := q.addTextColumn("claim_token"); err != nil {
		return err
	}
	if err := q.addTextColumn("user_id"); err != nil {
		return err
	}
	if err := q.addTextColumn("last_error"); err != nil {
		return err
	}
	if err := q.addTextColumn("unique_key"); err != nil {
		return err
	}
	if err := q.addTextColumn("sequence_key"); err != nil {
		return err
	}
	if err := q.addColumn("max_age_ns", q.intType()+" NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for _, ix := range []struct{ name, cols string }{
		{q.indexName("_dequeue_idx"), "(status, scheduled_at, priority)"},
		{q.indexName("_lane_idx"), "(lane, status, scheduled_at, priority)"},
		{q.indexName("_sequence_idx"), "(sequence_key, status, created_at) WHERE sequence_key <> ''"},
	} {
		safeIdx, err := query.SafeIdent(ix.name)
		if err != nil {
			return fmt.Errorf("queue: invalid index name %q: %w", ix.name, err)
		}
		idx := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s %s",
			query.IndexOn(q.dialect == dialectSQLite, query.QuoteIndexName(safeIdx), q.qt()), ix.cols)
		if _, err := q.db.Exec(idx); err != nil {
			return err
		}
	}
	safeIdx, err := query.SafeIdent(q.indexName("_unique_idx"))
	if err != nil {
		return fmt.Errorf("queue: invalid index name: %w", err)
	}
	if _, err := q.db.Exec(fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (type, unique_key) WHERE %s",
		query.IndexOn(q.dialect == dialectSQLite, query.QuoteIndexName(safeIdx), q.qt()), liveUniqueKey)); err != nil {
		return err
	}
	seqIdx, err := query.SafeIdent(q.indexName("_sequence_claim_idx"))
	if err != nil {
		return fmt.Errorf("queue: invalid index name: %w", err)
	}
	_, err = q.db.Exec(fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (sequence_key) WHERE sequence_key <> '' AND status = 'claimed'",
		query.IndexOn(q.dialect == dialectSQLite, query.QuoteIndexName(seqIdx), q.qt())))
	return err
}

const maxIdentLen = 63

func (q *DBQueue) indexName(suffix string) string {
	name := q.table + suffix
	if len(name) <= maxIdentLen {
		return name
	}
	sum := sha256.Sum256([]byte(q.table))
	tag := "_" + hex.EncodeToString(sum[:4])
	return q.table[:maxIdentLen-len(suffix)-len(tag)] + tag + suffix
}

const liveUniqueKey = "unique_key <> '' AND status IN ('pending', 'claimed')"

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "duplicate key value")
}

func (q *DBQueue) addTextColumn(column string) error {
	return q.addColumn(column, "TEXT NOT NULL DEFAULT ''")
}

func (q *DBQueue) addColumn(column, def string) error {
	ident, err := query.SafeIdent(column)
	if err != nil {
		return fmt.Errorf("queue: add column %q: %w", column, err)
	}
	if q.dialect == dialectPostgres {
		_, err := q.db.Exec(fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", q.qt(), ident, def))
		return err
	}
	_, err = q.db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s %s", q.qt(), ident, def))
	if err != nil && isDuplicateColumnErr(err) {
		return nil
	}
	return err
}

func (q *DBQueue) intType() string {
	if q.dialect == dialectPostgres {
		return "BIGINT"
	}
	return "INTEGER"
}

func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}

// RegisterHandler binds a job type to a handler.
func (q *DBQueue) RegisterHandler(jobType string, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[jobType] = h
}

// SetLeaseTimeout adjusts the in-flight lease after construction.
func (q *DBQueue) SetLeaseTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	if err := leaseCoversBudget(d, q.handlerTimeout); err != nil {
		q.logger.Warn("queue: SetLeaseTimeout ignored; the lease is unchanged", "err", err.Error())
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lease = d
}

func leaseCoversBudget(lease, handlerTimeout time.Duration) error {
	if handlerTimeout > 0 && lease <= handlerTimeout {
		return fmt.Errorf("queue: the claim lease (%s) must be longer than the handler budget WithDBHandlerTimeout(%s): "+
			"a handler still inside its budget would have its job re-claimed and run again; raise WithLeaseTimeout above it",
			lease, handlerTimeout)
	}
	return nil
}

// SetGate installs a gate the worker loop checks after it resolves a handler.
func (q *DBQueue) SetGate(gate func(jobType string) bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gate = gate
}

func (q *DBQueue) release(ctx context.Context, job Job) error {
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := q.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET status='pending',
				attempts = CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END,
				scheduled_at = $1, updated_at = $2
				WHERE id = $3 AND status='claimed' AND claim_token = $4`, q.qt()),
			q.now().UTC().Add(gateDeferDelay), q.now().UTC(), job.ID, job.ClaimToken)
		return err
	})
}

const gateDeferDelay = 100 * time.Millisecond

func (q *DBQueue) leaseTimeout() time.Duration {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.lease
}

func (q *DBQueue) handlerFor(jobType string) (Handler, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	h, ok := q.handlers[jobType]
	return h, ok
}

func (q *DBQueue) handlerTypes() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return keys(q.handlers)
}

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
		if gateAllows(q.logger, gate, t, "") {
			out = append(out, t)
		}
	}
	return out
}

func gateAllows(logger *slog.Logger, gate func(jobType string) bool, jobType, jobID string) (allow bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("queue: gate panic; failing closed",
				"job_type", jobType,
				"job_id", jobID,
				"panic", textsafe.Recovered(r))
			allow = false
		}
	}()
	return gate(jobType)
}

// Enqueue inserts a job.
func (q *DBQueue) Enqueue(ctx context.Context, job Job) error {
	return q.enqueueWith(ctx, q.db, job)
}

// Dequeue claims the highest-priority eligible job in one atomic step.
func (q *DBQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	return q.dequeue(ctx, "", types)
}

func (q *DBQueue) dequeue(ctx context.Context, lane string, types []string) (Job, error) {
	if err := q.deadLetterExpiredFinalClaims(ctx); err != nil {
		return Job{}, err
	}
	if err := q.pruneDone(ctx); err != nil {
		return Job{}, err
	}
	for range claimRetries {
		var job Job
		var err error
		switch q.dialect {
		case dialectPostgres:
			job, err = q.dequeuePostgres(ctx, lane, types)
		default:
			job, err = q.dequeueSQLite(ctx, lane, types)
		}
		if !isUniqueViolation(err) {
			return job, err
		}
	}
	return Job{}, ErrNoJob
}

const claimRetries = 3

func (q *DBQueue) deadLetterExpiredFinalClaims(ctx context.Context) error {
	now := q.now().UTC()
	cutoff := now.Add(-q.leaseTimeout())
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, type, attempts, max_attempts, max_age_ns, created_at FROM %s
		WHERE status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $1
		AND (attempts >= max_attempts OR max_age_ns > 0)`, q.qt()), cutoff)
	if err != nil {
		return err
	}
	type stranded struct {
		id, jobType         string
		attempts, maxAttspt int
		maxAge              time.Duration
		createdAt           any
	}
	var found []stranded
	for rows.Next() {
		var s stranded
		if err := rows.Scan(&s.id, &s.jobType, &s.attempts, &s.maxAttspt, &s.maxAge, &s.createdAt); err != nil {
			rows.Close()
			return err
		}
		found = append(found, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	finalStmt := fmt.Sprintf(
		`UPDATE %s SET status='failed', updated_at=$1, last_error=$2
		WHERE id=$3 AND status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $4
		AND attempts >= max_attempts`, q.qt())
	agedStmt := fmt.Sprintf(
		`UPDATE %s SET status='failed', updated_at=$1, last_error=$2
		WHERE id=$3 AND status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $4
		AND max_age_ns > 0`, q.qt())
	for _, s := range found {
		stmt, reason, logMsg := finalStmt, leaseExpiredError, "queue: lease expired on final attempt; job dead-lettered"
		if s.attempts < s.maxAttspt {
			created, err := query.ParseDBTime(s.createdAt)
			if err != nil || !pastMaxAge(Job{CreatedAt: created, MaxAge: s.maxAge}, now) {
				continue
			}
			stmt, reason, logMsg = agedStmt, leaseExpiredPastMaxAgeError, "queue: lease expired past MaxAge; job dead-lettered"
		}
		res, err := q.db.ExecContext(ctx, stmt, now, reason, s.id, cutoff)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			q.logger.Error(logMsg,
				"job_id", s.id,
				"job_type", s.jobType,
				"attempt", s.attempts,
				"max_attempts", s.maxAttspt)
		}
	}
	return nil
}

const leaseExpiredError = "lease expired on the final attempt: the worker stopped (crash, kill, or a handler that outran the lease) before it settled the job"

const leaseExpiredPastMaxAgeError = "lease expired past MaxAge: the worker stopped (crash, kill, or a handler that outran the lease) before it settled the job, and the job is too old to retry"

func (q *DBQueue) pruneDone(ctx context.Context) error {
	if q.doneRetention <= 0 {
		return nil
	}
	now := q.now().UTC()
	q.mu.Lock()
	due := q.lastPrune.IsZero() || now.Sub(q.lastPrune) >= donePruneEvery
	if due {
		q.lastPrune = now
	}
	q.mu.Unlock()
	if !due {
		return nil
	}
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE status='done' AND updated_at <= $1`, q.qt()), now.Add(-q.doneRetention))
	return err
}

func (q *DBQueue) dequeuePostgres(ctx context.Context, lane string, types []string) (Job, error) {
	where, args := q.eligibleWhere(types, 3, lane)
	token := redisRandomID()
	claimArgs := append([]any{q.now().UTC(), token}, args...)
	sqlStr := fmt.Sprintf(`UPDATE %s SET status='claimed', claimed_at=$1, updated_at=$1, claim_token=$2, attempts = attempts + 1
		WHERE id = (
			SELECT id FROM %s AS cand
			WHERE %s
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+jobColumns,
		q.qt(), q.qt(), where)
	row := q.db.QueryRowContext(ctx, sqlStr, claimArgs...)
	job, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	job.ClaimToken = token
	return job, nil
}

func (q *DBQueue) dequeueSQLite(ctx context.Context, lane string, types []string) (Job, error) {
	conn, err := q.db.Conn(ctx)
	if err != nil {
		return Job{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return Job{}, err
	}
	committed := false
	defer func() {
		if !committed {
			//gofastr:allow(poisonedconn) SQLite BEGIN IMMEDIATE claim: since SQLite 3.7.11 ROLLBACK aborts pending reads instead of failing, so its error here means no transaction is left active, and retiring the conn would only drop a :memory: database
			if _, err := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); err != nil {
				if q.logger != nil {
					q.logger.Warn("queue: rolling back an abandoned claim", "err", err)
				}
			}
		}
	}()
	tx := sqliteConnTx{conn: conn}

	where, args := q.eligibleWhere(types, 1, lane)
	pickSQL := fmt.Sprintf(`SELECT `+jobColumns+`
		FROM %s AS cand WHERE %s ORDER BY priority DESC, created_at ASC LIMIT 1`, q.qt(), where)
	row := tx.QueryRowContext(ctx, pickSQL, args...)
	job, err := scanJob(row)
	if err != nil {
		return Job{}, err
	}
	token := redisRandomID()
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET status='claimed', claimed_at=$1, updated_at=$1, claim_token=$2, attempts = attempts + 1 WHERE id = $3`, q.qt()),
		q.now().UTC(), token, job.ID,
	); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	committed = true
	job.Attempts++
	job.ClaimToken = token
	return job, nil
}

type sqliteConnTx struct{ conn *sql.Conn }

func (t sqliteConnTx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.conn.QueryRowContext(ctx, q, args...)
}

func (t sqliteConnTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.conn.ExecContext(ctx, q, args...)
}

func (t sqliteConnTx) Commit(ctx context.Context) error {
	_, err := t.conn.ExecContext(ctx, "COMMIT")
	return err
}

func (q *DBQueue) eligibleWhere(types []string, startIdx int, lane string) (string, []any) {
	var args []any
	now := q.now().UTC()
	idx := startIdx
	leaseIdx := idx
	args = append(args, now.Add(-q.leaseTimeout()))
	idx++
	schedIdx := idx
	args = append(args, now)
	idx++

	status := fmt.Sprintf(
		"(status='pending' OR (status='claimed' AND claimed_at IS NOT NULL AND claimed_at <= $%d AND attempts < max_attempts))",
		leaseIdx,
	)
	parts := []string{status, "scheduled_at <= $" + itoa(schedIdx), q.sequenceClear()}

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

func (q *DBQueue) sequenceClear() string {
	return fmt.Sprintf(`(cand.sequence_key = '' OR NOT EXISTS (
		SELECT 1 FROM %s AS seq
		WHERE seq.sequence_key = cand.sequence_key AND seq.id <> cand.id
		AND (seq.status = 'claimed' OR (cand.status = 'pending' AND seq.status = 'pending'
			AND (seq.created_at < cand.created_at OR (seq.created_at = cand.created_at AND seq.id < cand.id))))))`, q.qt())
}

// Ack removes the job, or marks it done under WithDoneRetention.
func (q *DBQueue) Ack(ctx context.Context, job Job) error {
	return sqlbusy.Retry(ctx, func(ctx context.Context) error {
		if q.doneRetention > 0 {
			_, err := q.db.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s SET status='done', updated_at=$1 WHERE id = $2 AND status='claimed' AND claim_token = $3`, q.qt()),
				q.now().UTC(), job.ID, job.ClaimToken)
			return err
		}
		_, err := q.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND status='claimed' AND claim_token = $2`, q.qt()),
			job.ID, job.ClaimToken)
		return err
	})
}

// Nack returns a claimed job to pending while it has attempts left, and
// dead-letters it on the last one.
func (q *DBQueue) Nack(ctx context.Context, job Job) error {
	_, err := q.settleFailure(ctx, job, nil)
	return err
}

func lastErrorText(reason error) string {
	if reason == nil {
		return ""
	}
	msg := textsafe.ScrubControlBytes(reason.Error())
	if r := []rune(msg); len(r) > maxLastError {
		msg = string(r[:maxLastError-1]) + "…"
	}
	return msg
}

func (q *DBQueue) settleFailure(ctx context.Context, job Job, reason error) (dead bool, err error) {
	now := q.now().UTC()
	lastError := lastErrorText(reason)
	var stored Job
	var createdAt any
	err = sqlbusy.Retry(ctx, func(ctx context.Context) error {
		row := q.db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT attempts, max_attempts, max_age_ns, created_at FROM %s WHERE id = $1 AND status='claimed' AND claim_token = $2", q.qt()),
			job.ID, job.ClaimToken)
		return row.Scan(&stored.Attempts, &stored.MaxAttempts, &stored.MaxAge, &createdAt)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if stored.CreatedAt, err = query.ParseDBTime(createdAt); err != nil {
		return false, fmt.Errorf("queue: decode job %q created_at: %w", job.ID, err)
	}
	dead = stored.Attempts >= stored.MaxAttempts || errors.Is(reason, ErrPermanent)
	next := now
	if !dead {
		if d, ok := retryDelayFrom(reason); ok {
			next = now.Add(d)
		} else {
			next = now.Add(q.retryDelay(stored.Attempts))
		}
		dead = pastMaxAge(stored, next)
	}
	if dead {
		stmt := fmt.Sprintf(`UPDATE %s SET status='failed', updated_at = $1,
			last_error = CASE WHEN $2 = '' THEN last_error ELSE $2 END
			WHERE id = $3 AND status='claimed' AND claim_token = $4`, q.qt())
		n, err := sqlbusy.Exec(ctx, func(ctx context.Context) (int64, error) {
			res, err := q.db.ExecContext(ctx, stmt, now, lastError, job.ID, job.ClaimToken)
			if err != nil {
				return 0, err
			}
			return res.RowsAffected()
		})
		if err != nil {
			return false, err
		}
		return n > 0, nil
	}
	stmt := fmt.Sprintf(`UPDATE %s SET status='pending', scheduled_at=$1, updated_at = $2,
		last_error = CASE WHEN $3 = '' THEN last_error ELSE $3 END
		WHERE id = $4 AND status='claimed' AND claim_token = $5`, q.qt())
	err = sqlbusy.Retry(ctx, func(ctx context.Context) error {
		_, err := q.db.ExecContext(ctx, stmt, next, now, lastError, job.ID, job.ClaimToken)
		return err
	})
	return false, err
}

// Replay implements Replayable.
func (q *DBQueue) Replay(ctx context.Context, jobID string) error {
	_, err := q.ReplayJob(ctx, jobID)
	return err
}

// ReplayJob implements ReplayReporter.
func (q *DBQueue) ReplayJob(ctx context.Context, jobID string) (bool, error) {
	stmt := fmt.Sprintf("UPDATE %s SET status='pending', attempts=0, scheduled_at=$1, updated_at=$1 WHERE id=$2 AND status='failed'", q.qt())
	res, err := q.db.ExecContext(ctx, stmt, q.now().UTC(), jobID)
	if isUniqueViolation(err) {
		return false, fmt.Errorf("%w: replay %s", ErrDuplicateJob, jobID)
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ListJobs implements Browsable.
func (q *DBQueue) ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	offset = max(offset, 0)
	base := fmt.Sprintf(`SELECT id, occurrence_id, type, payload, priority, lane, attempts,
		max_attempts, created_at, scheduled_at, status, updated_at, last_error, unique_key, max_age_ns, sequence_key, user_id FROM %s`, q.qt())
	args := []any{}
	if status != "" && status != "all" {
		base += " WHERE status = $1"
		args = append(args, status)
	}
	base += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT %d OFFSET %d", limit, offset)
	rows, err := q.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var payload sql.NullString
		var createdAt, scheduledAt, updatedAt any
		if err := rows.Scan(&j.ID, &j.OccurrenceID, &j.Type, &payload, &j.Priority,
			&j.Lane, &j.Attempts, &j.MaxAttempts, &createdAt, &scheduledAt,
			&j.Status, &updatedAt, &j.LastError, &j.UniqueKey, &j.MaxAge, &j.SequenceKey, &j.UserID); err != nil {
			return nil, err
		}
		j.CreatedAt, err = query.ParseDBTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("queue: decode job %q created_at: %w", j.ID, err)
		}
		j.ScheduledAt, err = query.ParseDBTime(scheduledAt)
		if err != nil {
			return nil, fmt.Errorf("queue: decode job %q scheduled_at: %w", j.ID, err)
		}
		j.UpdatedAt = j.CreatedAt
		if updatedAt != nil {
			if j.UpdatedAt, err = query.ParseDBTime(updatedAt); err != nil {
				return nil, fmt.Errorf("queue: decode job %q updated_at: %w", j.ID, err)
			}
		}
		if payload.Valid {
			j.Payload = []byte(payload.String)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Stats implements Browsable.
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

var (
	_ Queue      = (*DBQueue)(nil)
	_ Browsable  = (*DBQueue)(nil)
	_ Replayable = (*DBQueue)(nil)

	_ ReplayReporter = (*DBQueue)(nil)
)

// Close stops worker goroutines started by Start. Idempotent.
func (q *DBQueue) Close() error {
	q.mu.Lock()
	select {
	case <-q.stop:
		q.mu.Unlock()
		return nil
	default:
		close(q.stop)
	}
	started := q.started
	q.mu.Unlock()
	if !started {
		return nil
	}
	<-q.stopped
	return nil
}

// Start launches the shared workers and one goroutine per lane worker.
func (q *DBQueue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	select {
	case <-q.stop:
		q.mu.Unlock()
		return
	default:
	}
	q.started = true
	q.mu.Unlock()

	laneCount := 0
	for _, n := range q.laneWorkers {
		laneCount += n
	}
	total := q.workers + laneCount
	go func() {
		defer close(q.stopped)
		done := make(chan struct{}, total)
		for i := 0; i < q.workers; i++ {
			go q.superviseWorker(ctx, "", done)
		}
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

func (q *DBQueue) sortedLanes() []string {
	lanes := make([]string, 0, len(q.laneWorkers))
	for lane := range q.laneWorkers {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	return lanes
}

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
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-q.stop:
			timer.Stop()
			done <- struct{}{}
			return
		case <-ctx.Done():
			timer.Stop()
			done <- struct{}{}
			return
		case <-timer.C:
		}
	}
}

func (q *DBQueue) runWorker(ctx context.Context, lane string) (clean bool) {
	defer func() {
		if r := recover(); r != nil {
			q.logger.Error("queue: worker panic recovered; respawning after backoff",
				"lane", textsafe.SanitizeControlBytes(lane), "panic", textsafe.Recovered(r), "stack", string(debug.Stack()))
			clean = false
		}
	}()
	q.workerLoop(ctx, lane)
	return true
}

func (q *DBQueue) workerLoop(ctx context.Context, lane string) {
	backoff := 100 * time.Millisecond
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
		types := q.eligibleTypes()
		if len(types) == 0 {
			if wait() {
				return
			}
			continue
		}
		job, err := q.dequeue(ctx, lane, types)
		if err != nil {
			if !errors.Is(err, ErrNoJob) && ctx.Err() == nil {
				q.noteClaimErr(err)
			} else if errors.Is(err, ErrNoJob) {
				q.noteClaimOK()
			}
			if wait() {
				return
			}
			continue
		}
		q.noteClaimOK()
		if q.afterClaim != nil {
			//gofastr:allow(recovercallback) package-test seam, nil in production; a panicking test hook should fail its test
			q.afterClaim(job)
		}
		h, ok := q.handlerFor(job.Type)
		if !ok {
			settleCtx, cancel := settleContext(ctx)
			if dead, err := q.settleFailure(settleCtx, job, fmt.Errorf("%w: no handler registered for job type %q", ErrPermanent, job.Type)); err != nil {
				q.logger.Warn("queue: dead-lettering a job with no handler failed; it reruns when its lease expires",
					"job_id", job.ID, "err", err)
			} else if dead {
				q.logger.Error("queue: no handler registered; job dead-lettered",
					"job_id", job.ID, "job_type", job.Type)
			}
			cancel()
			continue
		}
		q.mu.RLock()
		gate := q.gate
		q.mu.RUnlock()
		if gate != nil && !gateAllows(q.logger, gate, job.Type, job.ID) {
			q.handBack(ctx, job)
			continue
		}
		if ctx.Err() != nil {
			q.handBack(ctx, job)
			return
		}
		if err := q.runHandler(ctx, h, job); err != nil {
			if ctx.Err() != nil && !errors.Is(err, ErrPermanent) {
				q.logger.Info("queue: shutdown interrupted a handler; job handed back",
					"job_id", job.ID, "job_type", job.Type, "err", err)
				q.handBack(ctx, job)
				return
			}
			q.logger.Warn("queue: handler failed",
				"job_id", job.ID,
				"job_type", job.Type,
				"attempt", job.Attempts,
				"max_attempts", job.MaxAttempts,
				"err", err)
			settleCtx, cancel := settleContext(ctx)
			dead, serr := q.settleFailure(settleCtx, job, err)
			cancel()
			if serr != nil {
				q.logger.Warn("queue: nack failed", "job_id", job.ID, "err", serr)
			} else if dead {
				q.logger.Error("queue: job dead-lettered",
					"job_id", job.ID,
					"job_type", job.Type,
					"attempt", job.Attempts,
					"max_attempts", job.MaxAttempts,
					"err", err)
			}
			continue
		}
		settleCtx, cancel := settleContext(ctx)
		if err := q.Ack(settleCtx, job); err != nil {
			q.logger.Warn("queue: ack failed", "job_id", job.ID, "err", err)
		}
		cancel()
	}
}

func (q *DBQueue) handBack(ctx context.Context, job Job) {
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if err := q.release(settleCtx, job); err != nil {
		q.logger.Warn("queue: handing back an unstarted job failed; it reruns when its lease expires",
			"job_id", job.ID, "err", err)
	}
}

func (q *DBQueue) noteClaimErr(err error) {
	if q.claimFailing.CompareAndSwap(false, true) {
		q.logger.Error("queue: claiming jobs failed; no job runs until this clears (workers retry every poll)",
			"table", q.table, "err", err)
	}
}

func (q *DBQueue) noteClaimOK() {
	if q.claimFailing.CompareAndSwap(true, false) {
		q.logger.Info("queue: claiming jobs recovered", "table", q.table)
	}
}

const settleTimeout = 10 * time.Second

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

func (q *DBQueue) runHandler(ctx context.Context, h Handler, job Job) (err error) {
	q.mu.RLock()
	timeout := q.handlerTimeout
	q.mu.RUnlock()
	if timeout <= 0 {
		return invokeRecovering(ctx, h, job)
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct{ err error }
	done := make(chan outcome, 1)
	go func() { done <- outcome{invokeRecovering(hctx, h, job)} }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.err
	case <-timer.C:
		return fmt.Errorf("queue: handler for %q exceeded its %s budget (context cancelled; job nacked and retried)",
			job.Type, timeout)
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

const jobColumns = "id, occurrence_id, type, payload, priority, lane, attempts, max_attempts, created_at, scheduled_at, user_id, unique_key, max_age_ns, sequence_key"

func scanJob(row interface {
	Scan(dest ...any) error
}) (Job, error) {
	var job Job
	var payload sql.NullString
	var lane sql.NullString
	var userID sql.NullString
	var createdAt, scheduledAt any
	if err := row.Scan(
		&job.ID, &job.OccurrenceID, &job.Type, &payload,
		&job.Priority, &lane, &job.Attempts, &job.MaxAttempts,
		&createdAt, &scheduledAt, &userID, &job.UniqueKey,
		&job.MaxAge, &job.SequenceKey,
	); err != nil {
		if err == sql.ErrNoRows {
			return Job{}, ErrNoJob
		}
		return Job{}, err
	}
	var err error
	job.CreatedAt, err = query.ParseDBTime(createdAt)
	if err != nil {
		return Job{}, fmt.Errorf("queue: decode job %q created_at: %w", job.ID, err)
	}
	job.ScheduledAt, err = query.ParseDBTime(scheduledAt)
	if err != nil {
		return Job{}, fmt.Errorf("queue: decode job %q scheduled_at: %w", job.ID, err)
	}
	if payload.Valid && payload.String != "" {
		job.Payload = json.RawMessage(payload.String)
	}
	if lane.Valid {
		job.Lane = lane.String
	}
	if userID.Valid {
		job.UserID = userID.String
	}
	return job, nil
}

func (q *DBQueue) sweepUserJobs(ctx context.Context, userID string) (int, error) {
	res, err := q.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE user_id = $1`, q.qt()), userID)
	if err != nil {
		return 0, fmt.Errorf("queue: erase %s jobs: %w", q.table, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("queue: erase %s jobs: %w", q.table, err)
	}
	return int(n), nil
}
