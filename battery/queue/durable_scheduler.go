package queue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/cron"
)

const durableSchedulerLeaseName = "scheduler"

// DurableSchedulerConfig identifies one scheduler replica and controls how
// quickly another replica may reclaim its leadership lease after a heartbeat
// stops. OwnerID defaults to a random process-local ID. LeaseDuration defaults
// to 30 seconds. OccurrenceRetention defaults to 30 days; a negative value
// disables occurrence pruning. MaxCatchUpOccurrences defaults to 1000 and
// bounds the history rows materialized after downtime.
type DurableSchedulerConfig struct {
	OwnerID               string
	LeaseDuration         time.Duration
	OccurrenceRetention   time.Duration
	MaxCatchUpOccurrences int
}

// DurableScheduler persists schedule watermarks and occurrences in the same
// SQL database as a DBQueue. A heartbeat-expiry lease avoids redundant
// evaluation, while unique occurrences and transactionally enqueued jobs are
// the deduplication authority.
type DurableScheduler struct {
	queue         *DBQueue
	ownerID       string
	leaseDuration time.Duration
	wake          chan struct{}

	occurrenceRetention   time.Duration
	maxCatchUpOccurrences int
	retentionMu           sync.Mutex
	nextRetentionSweep    time.Time
	// beforeOccurrenceCommit is a deterministic partition hook for package
	// tests. Production code leaves it nil.
	beforeOccurrenceCommit func()
}

// DurableScheduleBuilder configures one persisted recurring schedule.
type DurableScheduleBuilder struct {
	scheduler   *DurableScheduler
	id          string
	interval    time.Duration
	cronSpec    string
	jobType     string
	payload     json.RawMessage
	lane        string
	priority    int
	maxAttempts int
}

type durableSchedule struct {
	id          string
	jobType     string
	payload     json.RawMessage
	interval    time.Duration
	cronSpec    string
	lane        string
	priority    int
	maxAttempts int
	nextRun     time.Time
	updatedAt   time.Time
	version     int64
}

// NewDurableScheduler creates a replica-safe scheduler backed by q's SQL
// database. It creates the schedule, occurrence, and lease tables when absent.
func NewDurableScheduler(q *DBQueue, cfg DurableSchedulerConfig) (*DurableScheduler, error) {
	if q == nil || q.db == nil {
		return nil, errors.New("queue: durable scheduler requires a DBQueue")
	}
	if cfg.OwnerID == "" {
		cfg.OwnerID = randomID()
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 30 * time.Second
	}
	if cfg.OccurrenceRetention == 0 {
		cfg.OccurrenceRetention = defaultOccurrenceRetention
	}
	if cfg.MaxCatchUpOccurrences <= 0 {
		cfg.MaxCatchUpOccurrences = defaultMaxCatchUpOccurrences
	}
	s := &DurableScheduler{
		queue:                 q,
		ownerID:               cfg.OwnerID,
		leaseDuration:         cfg.LeaseDuration,
		occurrenceRetention:   cfg.OccurrenceRetention,
		maxCatchUpOccurrences: cfg.MaxCatchUpOccurrences,
		wake:                  make(chan struct{}, 1),
	}
	if err := s.ensureTables(); err != nil {
		return nil, fmt.Errorf("queue: ensure durable scheduler tables: %w", err)
	}
	return s, nil
}

// Every defines a durable fixed-interval schedule. id is the stable identity
// used to preserve its watermark across restarts and replica registration.
func (s *DurableScheduler) Every(id string, interval time.Duration) *DurableScheduleBuilder {
	return &DurableScheduleBuilder{scheduler: s, id: id, interval: interval}
}

// Cron defines a durable cron schedule. id is the stable identity used to
// preserve its watermark across restarts and replica registration.
func (s *DurableScheduler) Cron(id, spec string) *DurableScheduleBuilder {
	return &DurableScheduleBuilder{scheduler: s, id: id, cronSpec: spec}
}

// Job sets the queue job type and JSON payload for a durable schedule.
func (b *DurableScheduleBuilder) Job(jobType string, payload any) *DurableScheduleBuilder {
	b.jobType = jobType
	if payload == nil {
		b.payload = json.RawMessage("null")
		return b
	}
	switch v := payload.(type) {
	case json.RawMessage:
		b.payload = v
	case []byte:
		b.payload = json.RawMessage(v)
	case string:
		b.payload = json.RawMessage(v)
	default:
		data, _ := json.Marshal(payload)
		b.payload = data
	}
	return b
}

// Lane routes every Job fired by this schedule to the given capacity-
// reservation lane (see DBQueue.WithDBLaneWorkers). Empty (the default)
// lands the job on the shared/default lane. The value persists alongside
// the schedule definition; re-registering the same schedule ID updates it.
func (b *DurableScheduleBuilder) Lane(name string) *DurableScheduleBuilder {
	b.lane = name
	return b
}

// Priority sets the priority carried into every Job fired by this schedule.
// Higher integers are dequeued first. Defaults to 0. The value persists
// alongside the schedule definition; re-registering the same schedule ID
// updates it.
func (b *DurableScheduleBuilder) Priority(p int) *DurableScheduleBuilder {
	b.priority = p
	return b
}

// MaxAttempts bounds the retry ceiling carried into every Job fired by this
// schedule. Zero (the default) lets Enqueue resolve it to 3 — matching the
// pre-options behaviour. The value persists alongside the schedule
// definition; re-registering the same schedule ID updates it.
func (b *DurableScheduleBuilder) MaxAttempts(n int) *DurableScheduleBuilder {
	b.maxAttempts = n
	return b
}

// Register persists the schedule definition without resetting an existing
// watermark.
func (b *DurableScheduleBuilder) Register() error {
	return b.RegisterAt(time.Now())
}

// RegisterAt is Register with a deterministic first-run anchor.
func (b *DurableScheduleBuilder) RegisterAt(base time.Time) error {
	if b.scheduler == nil {
		return errors.New("queue: durable schedule has no scheduler")
	}
	if b.id == "" {
		return errors.New("queue: durable schedule requires a stable id")
	}
	if b.jobType == "" {
		return errors.New("queue: durable schedule requires a job type")
	}
	var next time.Time
	if b.cronSpec != "" {
		sc, err := cron.Parse(b.cronSpec)
		if err != nil {
			return err
		}
		next = sc.Next(base)
	} else {
		if b.interval <= 0 {
			return errors.New("queue: durable interval must be positive")
		}
		next = base.Add(b.interval)
	}
	payload := string(b.payload)
	if payload == "" {
		payload = "null"
	}
	_, err := b.scheduler.queue.db.Exec(
		fmt.Sprintf(`INSERT INTO %s
			(id, job_type, payload, interval_ns, cron_spec,
			 lane, priority, max_attempts,
			 next_run, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT(id) DO UPDATE SET
				job_type=excluded.job_type,
				payload=excluded.payload,
				interval_ns=excluded.interval_ns,
				cron_spec=excluded.cron_spec,
				lane=excluded.lane,
				priority=excluded.priority,
				max_attempts=excluded.max_attempts,
				updated_at=excluded.updated_at,
				version=%s.version+1`,
			b.scheduler.queue.schedulerSchedulesTable(),
			b.scheduler.queue.schedulerSchedulesTable()),
		b.id, b.jobType, payload, int64(b.interval), b.cronSpec,
		b.lane, b.priority, b.maxAttempts,
		next.UTC(), time.Now().UTC(),
	)
	if err == nil {
		select {
		case b.scheduler.wake <- struct{}{}:
		default:
		}
	}
	return err
}

// RunOnce evaluates every due schedule at now. A replica that does not own
// the current fenced lease returns nil without side effects.
func (s *DurableScheduler) RunOnce(ctx context.Context, now time.Time) error {
	_, err := s.runOnce(ctx, now)
	return err
}

func (s *DurableScheduler) runOnce(ctx context.Context, now time.Time) (bool, error) {
	now = now.UTC()
	fence, owned, err := s.acquireLease(ctx, now)
	if err != nil || !owned {
		return false, err
	}
	due, err := s.loadDue(ctx, now)
	if err != nil {
		return true, err
	}
	for _, schedule := range due {
		ticks, nextRun, err := schedule.dueTicks(now, s.maxCatchUpOccurrences)
		if err != nil {
			return true, err
		}
		if len(ticks) == 0 {
			continue
		}
		if s.beforeOccurrenceCommit != nil {
			s.beforeOccurrenceCommit()
		}
		if err := s.commitOccurrences(ctx, schedule, ticks, nextRun, fence, now); err != nil {
			return true, err
		}
	}
	if err := s.sweepOccurrences(ctx, now); err != nil {
		return true, err
	}
	return true, nil
}

// Start runs durable evaluation until ctx is cancelled. Registration wakes the
// loop immediately. The lease heartbeat is the maximum sleep; the earliest
// persisted next-run timestamp wakes it sooner.
func (s *DurableScheduler) Start(ctx context.Context) error {
	heartbeat := s.leaseDuration / 3
	if heartbeat <= 0 {
		heartbeat = 10 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.wake:
		case <-timer.C:
		}
		now := time.Now()
		owned, err := s.runOnce(ctx, now)
		if err != nil {
			return err
		}
		delay := heartbeat
		if owned {
			delay, err = s.nextWakeDelay(ctx, now, heartbeat)
			if err != nil {
				return err
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

func (s *DurableScheduler) nextWakeDelay(ctx context.Context, now time.Time, maximum time.Duration) (time.Duration, error) {
	var nextRaw any
	err := s.queue.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT next_run FROM %s ORDER BY next_run LIMIT 1", s.queue.schedulerSchedulesTable()),
	).Scan(&nextRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return maximum, nil
		}
		return 0, err
	}
	next, err := queueTime(nextRaw)
	if err != nil {
		return 0, fmt.Errorf("queue: decode scheduler next_run: %w", err)
	}
	delay := next.UTC().Sub(now.UTC())
	if delay < 10*time.Millisecond {
		return 10 * time.Millisecond, nil
	}
	if delay < maximum {
		return delay, nil
	}
	return maximum, nil
}

func (s *DurableScheduler) acquireLease(ctx context.Context, now time.Time) (int64, bool, error) {
	table := s.queue.schedulerLeaseTable()
	expires := now.Add(s.leaseDuration)
	res, err := s.queue.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(name, owner_id, fence, expires_at, heartbeat_at)
			VALUES ($1,$2,1,$3,$4)
			ON CONFLICT(name) DO NOTHING`, table),
		durableSchedulerLeaseName, s.ownerID, expires, now,
	)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return 1, true, nil
	}

	// An expired lease always issues a new monotonically increasing fence,
	// including when the previous owner happens to use the same owner ID.
	res, err = s.queue.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s
			SET owner_id=$1, fence=fence+1, expires_at=$2, heartbeat_at=$3
			WHERE name=$4 AND expires_at <= $3`, table),
		s.ownerID, expires, now, durableSchedulerLeaseName,
	)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// The current owner may heartbeat without issuing a new fence.
		res, err = s.queue.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s
				SET expires_at=$1, heartbeat_at=$2
				WHERE name=$3 AND owner_id=$4 AND expires_at > $2`, table),
			expires, now, durableSchedulerLeaseName, s.ownerID,
		)
		if err != nil {
			return 0, false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return 0, false, nil
		}
	}
	var owner string
	var fence int64
	err = s.queue.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT owner_id, fence FROM %s WHERE name=$1", table),
		durableSchedulerLeaseName,
	).Scan(&owner, &fence)
	if err != nil {
		return 0, false, err
	}
	return fence, owner == s.ownerID, nil
}

func (s *DurableScheduler) loadDue(ctx context.Context, now time.Time) ([]durableSchedule, error) {
	rows, err := s.queue.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, job_type, payload, interval_ns, cron_spec,
				lane, priority, max_attempts,
				next_run, updated_at, version
			FROM %s WHERE next_run <= $1 ORDER BY next_run, id`,
			s.queue.schedulerSchedulesTable()),
		now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []durableSchedule
	for rows.Next() {
		var row durableSchedule
		var payload string
		var intervalNS int64
		var nextRun, updatedAt any
		if err := rows.Scan(&row.id, &row.jobType, &payload, &intervalNS,
			&row.cronSpec, &row.lane, &row.priority, &row.maxAttempts,
			&nextRun, &updatedAt, &row.version); err != nil {
			return nil, err
		}
		row.payload = json.RawMessage(payload)
		row.interval = time.Duration(intervalNS)
		row.nextRun, err = queueTime(nextRun)
		if err != nil {
			return nil, fmt.Errorf("queue: decode schedule %q next_run: %w", row.id, err)
		}
		row.updatedAt, err = queueTime(updatedAt)
		if err != nil {
			return nil, fmt.Errorf("queue: decode schedule %q updated_at: %w", row.id, err)
		}
		row.nextRun = row.nextRun.UTC()
		row.updatedAt = row.updatedAt.UTC()
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s durableSchedule) dueTicks(now time.Time, limit int) ([]time.Time, time.Time, error) {
	return boundedDueTicks(s, now, limit)
}

func (s *DurableScheduler) commitOccurrences(
	ctx context.Context,
	schedule durableSchedule,
	ticks []time.Time,
	nextRun time.Time,
	fence int64,
	now time.Time,
) error {
	tx, err := s.queue.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// This conditional write is both the ownership re-check and the lock that
	// prevents a lease handoff from racing the occurrence/job transaction.
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET heartbeat_at=heartbeat_at
			WHERE name=$1 AND owner_id=$2 AND fence=$3 AND expires_at > $4`,
			s.queue.schedulerLeaseTable()),
		durableSchedulerLeaseName, s.ownerID, fence, now,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil
	}

	res, err = tx.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET next_run=$1, updated_at=$2, version=version+1
			WHERE id=$3 AND version=$4`, s.queue.schedulerSchedulesTable()),
		nextRun.UTC(), now, schedule.id, schedule.version,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil
	}

	var activePrior int
	if err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s o
			JOIN %s j ON j.id=o.enqueued_job_id
			WHERE o.schedule_id=$1 AND j.status IN ('pending','claimed')`,
			s.queue.schedulerOccurrencesTable(), s.queue.qt()),
		schedule.id,
	).Scan(&activePrior); err != nil {
		return err
	}

	for i, tick := range ticks {
		occurrenceID := stableOccurrenceID(schedule.id, tick)
		status := "skipped"
		jobID := ""
		skipReason := "missed"
		if i == len(ticks)-1 {
			skipReason = "overlap"
			if activePrior == 0 {
				status = "enqueued"
				skipReason = ""
				jobID = occurrenceID
			}
		}
		res, err = tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s
				(occurrence_id, schedule_id, scheduled_tick, status,
				 skip_reason, claim_owner, claim_fence, created_at, enqueued_job_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
				ON CONFLICT(occurrence_id) DO NOTHING`,
				s.queue.schedulerOccurrencesTable()),
			occurrenceID, schedule.id, tick.UTC(), status, skipReason,
			s.ownerID, fence, now, jobID,
		)
		if err != nil {
			return err
		}
		inserted, _ := res.RowsAffected()
		if status != "enqueued" || inserted == 0 {
			continue
		}
		if err := s.queue.enqueueWith(ctx, tx, Job{
			ID:           jobID,
			OccurrenceID: occurrenceID,
			Type:         schedule.jobType,
			Payload:      schedule.payload,
			Lane:         schedule.lane,
			Priority:     schedule.priority,
			MaxAttempts:  schedule.maxAttempts,
			CreatedAt:    now,
			ScheduledAt:  tick,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stableOccurrenceID(scheduleID string, tick time.Time) string {
	sum := sha256.Sum256([]byte(scheduleID + "\x00" + tick.UTC().Format(time.RFC3339Nano)))
	return "occ_" + hex.EncodeToString(sum[:16])
}

func (q *DBQueue) schedulerSchedulesTable() string {
	return q.schedulerTable("scheduler_schedules")
}

func (q *DBQueue) schedulerOccurrencesTable() string {
	return q.schedulerTable("scheduler_occurrences")
}

func (q *DBQueue) schedulerLeaseTable() string {
	return q.schedulerTable("scheduler_leases")
}

func (q *DBQueue) schedulerTable(suffix string) string {
	name, err := query.SafeIdent(q.table + "_" + suffix)
	if err != nil {
		panic(err)
	}
	return query.QuoteIdent(name)
}

func (s *DurableScheduler) ensureTables() error {
	tsType := "DATETIME"
	if s.queue.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			job_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			interval_ns BIGINT NOT NULL DEFAULT 0,
			cron_spec TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 0,
			next_run %s NOT NULL,
			updated_at %s NOT NULL,
			version BIGINT NOT NULL DEFAULT 0
		)`, s.queue.schedulerSchedulesTable(), tsType, tsType),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			occurrence_id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL,
			scheduled_tick %s NOT NULL,
			status TEXT NOT NULL,
			claim_owner TEXT NOT NULL,
			skip_reason TEXT NOT NULL DEFAULT '',
			claim_fence BIGINT NOT NULL,
			created_at %s NOT NULL,
			enqueued_job_id TEXT NOT NULL DEFAULT '',
			UNIQUE(schedule_id, scheduled_tick)
		)`, s.queue.schedulerOccurrencesTable(), tsType, tsType),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			name TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			fence BIGINT NOT NULL,
			expires_at %s NOT NULL,
			heartbeat_at %s NOT NULL
		)`, s.queue.schedulerLeaseTable(), tsType, tsType),
	}
	for _, statement := range statements {
		if _, err := s.queue.db.Exec(statement); err != nil {
			return err
		}
	}
	if err := s.ensureHardeningSchema(); err != nil {
		return err
	}
	return nil
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (q *DBQueue) enqueueWith(ctx context.Context, exec contextExecer, job Job) error {
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
	_, err := exec.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(id, occurrence_id, type, payload, priority, lane, attempts,
			 max_attempts, created_at, scheduled_at, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending')`, q.qt()),
		job.ID, job.OccurrenceID, job.Type, payload, job.Priority, job.Lane,
		job.Attempts, job.MaxAttempts, job.CreatedAt, job.ScheduledAt,
	)
	return err
}
