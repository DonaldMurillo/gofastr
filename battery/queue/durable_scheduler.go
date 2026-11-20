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
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/cron"
)

const durableSchedulerLeaseName = "scheduler"

const schedulerJobSweepEraser = "queue_scheduler_swept_jobs"

// DurableSchedulerConfig identifies one scheduler replica and how soon another
// may take its lease.
type DurableSchedulerConfig struct {
	OwnerID               string
	LeaseDuration         time.Duration
	OccurrenceRetention   time.Duration
	MaxCatchUpOccurrences int
}

// DurableScheduler persists schedules, watermarks and occurrences beside a
// DBQueue, safe across replicas and restarts.
type DurableScheduler struct {
	queue         *DBQueue
	ownerID       string
	leaseDuration time.Duration
	wake          chan struct{}

	occurrenceRetention    time.Duration
	maxCatchUpOccurrences  int
	retentionMu            sync.Mutex
	nextRetentionSweep     time.Time
	beforeOccurrenceCommit func()
	passDone               func(now time.Time, delay time.Duration)
}

func (s *DurableScheduler) hookBeforeOccurrenceCommit() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("queue: durable scheduler hook panicked; occurrence commit continues",
				"panic", textsafe.Recovered(rec))
		}
	}()
	s.beforeOccurrenceCommit()
}

// DurableScheduleBuilder configures one persisted recurring schedule.
type DurableScheduleBuilder struct {
	scheduler   *DurableScheduler
	id          string
	interval    time.Duration
	cronSpec    string
	jobType     string
	payload     json.RawMessage
	payloadErr  error
	lane        string
	priority    int
	maxAttempts int
	userID      string
}
type durableSchedule struct {
	id          string
	jobType     string
	payload     json.RawMessage
	interval    time.Duration
	cronSpec    string
	tz          string
	lane        string
	priority    int
	maxAttempts int
	userID      string
	nextRun     time.Time
	updatedAt   time.Time
	version     int64
}

// NewDurableScheduler creates a replica-safe scheduler over q's database,
// creating its tables when absent.
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
	table := q.schedulerName("_scheduler_schedules")
	datexport.Register(datexport.DataExporter{
		Name:       datexport.ScopedName("queue_scheduler_schedules", table),
		Source:     "queue",
		Table:      table,
		PrimaryKey: "id",
		Columns: []string{
			"id", "job_type", "payload", "interval_ns", "cron_spec", "tz",
			"lane", "priority", "max_attempts", "user_id", "next_run", "updated_at", "version",
		},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name:   datexport.ScopedName("queue_scheduler_schedules", table),
		Source: "queue",
		Table:  table,
		Column: "user_id",
		Mode:   datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name:   datexport.ScopedName(schedulerJobSweepEraser, q.table),
		Source: "queue",
		Table:  q.table,
		Column: "user_id",
		Mode:   datexport.EraseDelete,
	})
	return s, nil
}

// Every defines a durable fixed-interval schedule under the stable id.
func (s *DurableScheduler) Every(id string, interval time.Duration) *DurableScheduleBuilder {
	return &DurableScheduleBuilder{scheduler: s, id: id, interval: interval}
}

// Cron defines a durable cron schedule under the stable id.
func (s *DurableScheduler) Cron(id, spec string) *DurableScheduleBuilder {
	return &DurableScheduleBuilder{scheduler: s, id: id, cronSpec: spec}
}

// Job sets the job type and JSON payload; a payload error is returned by
// Register.
func (b *DurableScheduleBuilder) Job(jobType string, payload any) *DurableScheduleBuilder {
	b.jobType = jobType
	b.payloadErr = nil
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
		data, err := json.Marshal(payload)
		if err != nil {
			b.payload = nil
			b.payloadErr = fmt.Errorf("queue: marshal durable schedule payload: %w", err)
			return b
		}
		b.payload = data
	}
	if len(b.payload) == 0 {
		b.payload = json.RawMessage("null")
		return b
	}
	if !json.Valid(b.payload) {
		b.payload = nil
		b.payloadErr = errors.New("queue: durable schedule payload is not valid JSON")
	}
	return b
}

// Lane routes every job this schedule fires to the given lane.
func (b *DurableScheduleBuilder) Lane(name string) *DurableScheduleBuilder {
	b.lane = name
	return b
}

// Priority sets the priority of every job this schedule fires.
func (b *DurableScheduleBuilder) Priority(p int) *DurableScheduleBuilder {
	b.priority = p
	return b
}

// MaxAttempts sets the retry ceiling of every job this schedule fires; zero
// means 3.
func (b *DurableScheduleBuilder) MaxAttempts(n int) *DurableScheduleBuilder {
	b.maxAttempts = n
	return b
}

// UserID names the person the schedule's payload is about; erasing them
// deletes the schedule.
func (b *DurableScheduleBuilder) UserID(id string) *DurableScheduleBuilder {
	b.userID = id
	return b
}

// Register persists the schedule without resetting an existing watermark.
func (b *DurableScheduleBuilder) Register() error {
	return b.RegisterAt(time.Now().UTC())
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
	if b.payloadErr != nil {
		return b.payloadErr
	}
	tz := tzName(base.Location())
	var next time.Time
	if b.cronSpec != "" {
		sc, err := cron.Parse(b.cronSpec)
		if err != nil {
			return err
		}
		next = nextFire(sc, durableSchedule{tz: tz}.location(), base)
		if next.IsZero() {
			return fmt.Errorf("queue: cron spec %q never fires", b.cronSpec)
		}
	} else {
		if b.interval < minDurableInterval {
			return fmt.Errorf("queue: durable interval %s is below the %s floor", b.interval, minDurableInterval)
		}
		next = base.Add(b.interval)
	}
	payload := string(b.payload)
	_, err := b.scheduler.queue.db.Exec(
		fmt.Sprintf(`INSERT INTO %s
			(id, job_type, payload, interval_ns, cron_spec, tz,
			 lane, priority, max_attempts, user_id,
			 next_run, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT(id) DO UPDATE SET
				job_type=excluded.job_type,
				payload=excluded.payload,
				interval_ns=excluded.interval_ns,
				cron_spec=excluded.cron_spec,
				tz=excluded.tz,
				lane=excluded.lane,
				priority=excluded.priority,
				max_attempts=excluded.max_attempts,
				user_id=excluded.user_id,
				updated_at=excluded.updated_at,
				version=%s.version+1`,
			b.scheduler.queue.schedulerSchedulesTable(),
			b.scheduler.queue.schedulerSchedulesTable()),
		b.id, b.jobType, payload, int64(b.interval), b.cronSpec, tz,
		b.lane, b.priority, b.maxAttempts, b.userID,
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

// RunOnce evaluates every due schedule at now; a replica without the lease
// does nothing.
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
			slog.Default().Error("queue: durable scheduler skipping schedule",
				"schedule_id", schedule.id, "err", err.Error())
			continue
		}
		if len(ticks) == 0 {
			if !nextRun.IsZero() && !nextRun.Equal(schedule.nextRun) {
				if err := s.commitOccurrences(ctx, schedule, nil, nextRun, fence, now); err != nil {
					return true, err
				}
			}
			continue
		}
		if s.beforeOccurrenceCommit != nil {
			s.hookBeforeOccurrenceCommit()
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

// Start runs durable evaluation until ctx is cancelled.
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
		now := s.queue.now()
		owned, err := s.runOnce(ctx, now)
		if err != nil {
			slog.Default().Error("queue: durable scheduler evaluation failed; retrying next heartbeat",
				"err", err.Error())
		}
		delay := heartbeat
		if owned {
			delay, err = s.nextWakeDelay(ctx, now, heartbeat)
			if err != nil {
				slog.Default().Error("queue: durable scheduler wake computation failed; using heartbeat",
					"err", err.Error())
				delay = heartbeat
			}
		}
		if s.passDone != nil {
			//gofastr:allow(recovercallback) package-test seam, nil in production; a panicking test hook should fail its test
			s.passDone(now, delay)
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
		fmt.Sprintf("SELECT next_run FROM %s WHERE next_run > $1 ORDER BY next_run LIMIT 1", s.queue.schedulerSchedulesTable()),
		now.UTC(),
	).Scan(&nextRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return maximum, nil
		}
		return 0, err
	}
	next, err := query.ParseDBTime(nextRaw)
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
		fmt.Sprintf(`SELECT id, job_type, payload, interval_ns, cron_spec, tz,
				lane, priority, max_attempts, user_id,
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
			&row.cronSpec, &row.tz, &row.lane, &row.priority, &row.maxAttempts, &row.userID,
			&nextRun, &updatedAt, &row.version); err != nil {
			slog.Default().Error("queue: durable scheduler skipping schedule",
				"schedule_id", row.id, "err", err.Error())
			continue
		}
		row.payload = json.RawMessage(payload)
		row.interval = time.Duration(intervalNS)
		next, err := query.ParseDBTime(nextRun)
		if err != nil {
			slog.Default().Error("queue: durable scheduler skipping schedule",
				"schedule_id", row.id, "err", fmt.Errorf("decode next_run: %w", err).Error())
			continue
		}
		updated, err := query.ParseDBTime(updatedAt)
		if err != nil {
			slog.Default().Error("queue: durable scheduler skipping schedule",
				"schedule_id", row.id, "err", fmt.Errorf("decode updated_at: %w", err).Error())
			continue
		}
		row.nextRun = next.UTC()
		row.updatedAt = updated.UTC()
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
			UserID:       schedule.userID,
			CreatedAt:    now,
			ScheduledAt:  earliestTime(tick, s.queue.now()),
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
	name, err := query.SafeIdent(q.schedulerName("_" + suffix))
	if err != nil {
		panic(err)
	}
	return query.QuoteFolded(name)
}

var schedulerSuffixes = []string{
	"_scheduler_schedules",
	"_scheduler_occurrences",
	"_scheduler_leases",
	"_scheduler_occurrences_schedule_job_idx",
	"_scheduler_occurrences_created_at_idx",
}

func (q *DBQueue) schedulerName(suffix string) string {
	name := q.table + suffix
	if q.dialect != dialectPostgres || len(name) <= maxIdentLen {
		return name
	}
	cut := pgCut(name)
	index := strings.HasSuffix(suffix, "_idx")
	for _, other := range schedulerSuffixes {
		if other == suffix || pgCut(q.table+other) != cut {
			continue
		}
		if index || !strings.HasSuffix(other, "_idx") {
			return q.indexName(suffix)
		}
	}
	return cut
}

func pgCut(name string) string {
	if len(name) > maxIdentLen {
		return name[:maxIdentLen]
	}
	return name
}

func (s *DurableScheduler) ensureTables() error {
	tsType := "DATETIME"
	if s.queue.dialect == dialectPostgres {
		tsType = "TIMESTAMPTZ"
	}
	statements := []string{
		//gofastr:allow(GOFASTR1415) NewDurableScheduler registers this table's eraser under its configured name, on user_id (added by ensureHardeningSchema), so the rule's static scan cannot see it
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY NOT NULL,
			job_type TEXT NOT NULL,
			payload TEXT NOT NULL,
			interval_ns BIGINT NOT NULL DEFAULT 0,
			cron_spec TEXT NOT NULL DEFAULT '',
			tz TEXT NOT NULL DEFAULT '',
			lane TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 0,
			user_id TEXT NOT NULL DEFAULT '',
			next_run %s NOT NULL,
			updated_at %s NOT NULL,
			version BIGINT NOT NULL DEFAULT 0
		)`, s.queue.schedulerSchedulesTable(), tsType, tsType),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			occurrence_id TEXT PRIMARY KEY NOT NULL,
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
		//gofastr:allow(GOFASTR1415) owner_id is the replica holding the scheduler lease, not a user; the table holds no user data
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			name TEXT PRIMARY KEY NOT NULL,
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
	if err := s.queue.normalizeLegacyTimestamps(context.Background()); err != nil {
		return fmt.Errorf("normalize scheduler legacy timestamps: %w", err)
	}
	return nil
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (q *DBQueue) enqueueWith(ctx context.Context, exec contextExecer, job Job) error {
	if job.MaxAge < 0 {
		return errNegativeMaxAge
	}
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
	onConflict := ""
	if job.UniqueKey != "" {
		onConflict = " ON CONFLICT (type, unique_key) WHERE " + liveUniqueKey + " DO NOTHING"
	}
	res, err := exec.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s
			(id, occurrence_id, type, payload, priority, lane, attempts,
			 max_attempts, created_at, scheduled_at, status, user_id, updated_at, unique_key,
			 max_age_ns, sequence_key)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11,$12,$13,$14,$15)%s`, q.qt(), onConflict),
		job.ID, job.OccurrenceID, job.Type, payload, job.Priority, job.Lane,
		job.Attempts, job.MaxAttempts, job.CreatedAt, job.ScheduledAt, job.UserID, now, job.UniqueKey,
		int64(job.MaxAge), job.SequenceKey,
	)
	if err != nil || job.UniqueKey == "" {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrDuplicateJob
	}
	return nil
}
