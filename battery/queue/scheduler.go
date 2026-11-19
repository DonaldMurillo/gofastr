package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// ScheduledJob defines a recurring job configuration.
//
// A schedule fires on either a fixed Interval (set via Scheduler.Every) or a
// cron expression (set via Scheduler.Cron). Interval and cron are mutually
// exclusive: when cron is non-nil the Interval field is unused and NextRun is
// advanced by the cron expression instead.
type ScheduledJob struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Interval time.Duration   `json:"interval"`
	NextRun  time.Time       `json:"next_run"`

	// Lane, Priority, and MaxAttempts are carried into every Job fired by
	// this schedule. They default to "", 0, and 0 (which enqueueWith
	// resolves to 3), matching the pre-options behaviour for callers that
	// never set them.
	Lane        string `json:"lane,omitempty"`
	Priority    int    `json:"priority"`
	MaxAttempts int    `json:"max_attempts"`

	// UserID is carried into every Job fired by this schedule (see
	// Job.UserID). Empty for a schedule that names nobody.
	UserID string `json:"user_id,omitempty"`

	// cron, when set, drives NextRun from a cron expression instead of
	// Interval. Nil for interval schedules (the pre-existing behaviour).
	cron *cron.Schedule
	// loc is the zone a cron schedule is evaluated in: the location of
	// the time it was registered at.
	loc *time.Location
}

// Scheduler is the non-durable, single-process scheduler. Its watermarks live
// only in memory. Use DurableScheduler with DBQueue when multiple replicas or
// restart continuity must be safe.
type Scheduler struct {
	mu        sync.Mutex
	queues    []Queue
	schedules []ScheduledJob
	logger    *slog.Logger

	// wake is signalled by Register so a running Start loop re-arms its
	// timer immediately, rather than waiting out a coarse (up to a minute)
	// poll before a newly-registered sub-minute schedule can fire.
	wake chan struct{}

	// eraseName is the name the store eraser newScheduler registers is
	// listed under; unregisterErase removes it, on Close.
	eraseName       string
	unregisterErase func()
}

// NewScheduler creates the non-durable, single-process Scheduler that
// dispatches to the given queues. Enqueue errors are logged via slog.Default().
// NewInMemoryScheduler is the explicit spelling for new code.
func NewScheduler(queues ...Queue) *Scheduler {
	return newScheduler(queues, slog.Default())
}

// newScheduler builds a Scheduler and registers its store eraser:
// App.EraseUserData drops the schedules whose UserID is the erased user,
// so their payload is neither kept nor fired again.
func newScheduler(queues []Queue, logger *slog.Logger) *Scheduler {
	s := &Scheduler{
		queues: queues,
		logger: logger,
		wake:   make(chan struct{}, 1),
	}
	s.eraseName, s.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "queue_scheduler_memory",
		Source: "queue",
		Erase:  s.eraseUser,
		// Erased before the queues it feeds: a tick between a queue's
		// erasure and this one enqueued the user's job again.
		Producer: true,
	})
	return s
}

// eraserName is the name the scheduler's store eraser is listed under.
func (s *Scheduler) eraserName() string { return s.eraseName }

// eraseUser drops every schedule whose UserID is userID and returns how
// many items it removed (with dryRun, how many schedules it would). It
// takes s.mu, which dispatchDue holds across its Enqueue calls, so a
// dispatch in progress has finished before the schedules go.
//
// Then it sweeps the user's jobs out of every DBQueue it feeds, and counts
// them. App.EraseUserData deletes queue_jobs rows in its SQL plan and runs
// store erasers only after the commit, so a tick between the two put the
// user's job back in the table; with the schedules gone first, the sweep
// leaves nothing a later tick re-creates. Stores erased after this one
// (MemoryQueue, RedisQueue) need no sweep: Producer orders this eraser
// ahead of them.
func (s *Scheduler) eraseUser(ctx context.Context, userID string, dryRun bool) (int, error) {
	s.mu.Lock()
	kept := make([]ScheduledJob, 0, len(s.schedules))
	for _, sch := range s.schedules {
		if sch.UserID != userID {
			kept = append(kept, sch)
		}
	}
	n := len(s.schedules) - len(kept)
	if !dryRun {
		s.schedules = kept
	}
	queues := slices.Clone(s.queues)
	s.mu.Unlock()
	if dryRun {
		return n, nil
	}
	var errs []error
	for _, q := range queues {
		sw, ok := q.(userJobSweeper)
		if !ok {
			continue
		}
		swept, err := sw.sweepUserJobs(ctx, userID)
		n += swept
		errs = append(errs, err)
	}
	return n, errors.Join(errs...)
}

// userJobSweeper is a SQL-backed queue whose rows the erasure's SQL plan
// already deleted once; the in-memory Scheduler sweeps it again after its
// own schedules are gone.
type userJobSweeper interface {
	sweepUserJobs(ctx context.Context, userID string) (int, error)
}

// Close removes the scheduler from App.EraseUserData's reach. It does not
// stop Start; cancel Start's context for that.
func (s *Scheduler) Close() error {
	if s.unregisterErase != nil {
		s.unregisterErase()
	}
	return nil
}

// NewInMemoryScheduler explicitly creates the non-durable, single-process
// scheduler. It is equivalent to NewScheduler.
func NewInMemoryScheduler(queues ...Queue) *Scheduler {
	return NewScheduler(queues...)
}

// NewSchedulerWithLogger creates a new Scheduler with an explicit logger.
// Pass a non-nil *slog.Logger to control where enqueue-error messages are
// routed; passing nil falls back to slog.Default().
func NewSchedulerWithLogger(q Queue, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return newScheduler([]Queue{q}, logger)
}

// Every returns a ScheduleBuilder that fires on a fixed interval. Register
// refuses a negative interval or a positive one below one millisecond, the
// DurableScheduler floor.
func (s *Scheduler) Every(interval time.Duration) *ScheduleBuilder {
	return &ScheduleBuilder{
		scheduler: s,
		interval:  interval,
	}
}

// Cron returns a ScheduleBuilder that fires when the cron expression's next
// time arrives. The spec accepts the standard 5-field syntax plus the
// @shortcuts (e.g. "0 2 * * *" for every day at 02:00, or "@daily"); it is
// parsed by framework/cron.Parse, so the queue does not carry a second cron
// parser. Spec errors surface from Register / RegisterAt, not here, so the
// fluent chain stays clean.
func (s *Scheduler) Cron(spec string) *ScheduleBuilder {
	return &ScheduleBuilder{
		scheduler: s,
		cronSpec:  spec,
		hasCron:   true,
	}
}

// ScheduleBuilder provides a fluent API for building scheduled jobs.
type ScheduleBuilder struct {
	scheduler   *Scheduler
	interval    time.Duration
	cronSpec    string
	hasCron     bool
	jobType     string
	payload     json.RawMessage
	payloadErr  error
	lane        string
	priority    int
	maxAttempts int
	userID      string
}

// encodeSchedulePayload converts a Job payload argument into the JSON a
// schedule stores: the byte-like spellings pass through, anything else
// marshals. label prefixes the error json.Marshal refuses.
func encodeSchedulePayload(payload any, label string) (json.RawMessage, error) {
	switch v := payload.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return json.RawMessage(v), nil
	case string:
		return json.RawMessage(v), nil
	default:
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		return data, nil
	}
}

// Job sets the job type and payload for the scheduled job. A payload
// json.Marshal refuses is returned by Register or RegisterAt, so the
// builder remains fluent. A nil payload means no payload: it replaces
// whatever an earlier Job call set.
func (b *ScheduleBuilder) Job(jobType string, payload any) *ScheduleBuilder {
	b.jobType = jobType
	b.payloadErr = nil
	if payload == nil {
		b.payload = nil
		return b
	}
	b.payload, b.payloadErr = encodeSchedulePayload(payload, "queue: marshal schedule payload")
	return b
}

// Register adds the scheduled job to the scheduler, computing the first
// NextRun relative to the current wall clock. It returns an error when no
// job type was set, the payload given to Job cannot be encoded as JSON or a
// Cron schedule's spec is invalid, and registers nothing then.
func (b *ScheduleBuilder) Register() error {
	return b.RegisterAt(time.Now())
}

// Lane routes every Job fired by this schedule to the given capacity-
// reservation lane (see DBQueue.WithDBLaneWorkers / MemoryQueue.WithLaneWorkers).
// Empty (the default) lands the job on the shared/default lane.
func (b *ScheduleBuilder) Lane(name string) *ScheduleBuilder {
	b.lane = name
	return b
}

// Priority sets the priority carried into every Job fired by this schedule.
// Higher integers are dequeued first. Defaults to 0.
func (b *ScheduleBuilder) Priority(p int) *ScheduleBuilder {
	b.priority = p
	return b
}

// MaxAttempts bounds the retry ceiling carried into every Job fired by this
// schedule. Zero (the default) lets Enqueue resolve it to 3, matching the
// pre-options behaviour.
func (b *ScheduleBuilder) MaxAttempts(n int) *ScheduleBuilder {
	b.maxAttempts = n
	return b
}

// UserID names the person the schedule's payload is about. Every Job it
// fires carries it as Job.UserID, which is what App.EraseUserData matches
// on in the queue the job lands in. The schedule itself lives in process
// memory, out of an erasure's reach; use a DurableScheduler for a
// schedule that must go with the user.
func (b *ScheduleBuilder) UserID(id string) *ScheduleBuilder {
	b.userID = id
	return b
}

// RegisterAt is like Register but anchors the first NextRun to base instead of
// the wall clock. It exists so cron schedules can be registered deterministically
// (tests, replayed fixtures) without depending on time.Now().
func (b *ScheduleBuilder) RegisterAt(base time.Time) error {
	if b.jobType == "" {
		return errors.New("queue: schedule requires a job type")
	}
	if b.payloadErr != nil {
		return b.payloadErr
	}

	job := ScheduledJob{
		Type:        b.jobType,
		Payload:     b.payload,
		Interval:    b.interval,
		Lane:        b.lane,
		Priority:    b.priority,
		MaxAttempts: b.maxAttempts,
		UserID:      b.userID,
	}

	if b.hasCron {
		sc, err := cron.Parse(b.cronSpec)
		if err != nil {
			return err
		}
		job.cron = &sc
		// The zone base carries is the one every later firing is read
		// in; nextFire gives a fixed-time spec vixie cron's handling
		// across a DST change (cron.Schedule.FiresAt).
		job.loc = base.Location()
		job.NextRun = nextFire(sc, job.loc, base)
		// Next returns the zero time for a spec that never fires
		// (February 30), and a zero NextRun is due on every tick.
		if job.NextRun.IsZero() {
			return fmt.Errorf("queue: cron spec %q never fires", b.cronSpec)
		}
	} else {
		// The DurableScheduler floor, so Every(5) (five nanoseconds: the
		// untyped constant compiles) cannot spin Start's timer loop into
		// enqueuing a job per iteration. A negative interval is refused
		// too. Zero keeps its old meaning: due on every tick, at the
		// one-minute floor.
		if b.interval < 0 || (b.interval > 0 && b.interval < minDurableInterval) {
			return fmt.Errorf("queue: schedule interval %s is below the %s floor", b.interval, minDurableInterval)
		}
		job.NextRun = base.Add(b.interval)
	}

	b.scheduler.mu.Lock()
	b.scheduler.schedules = append(b.scheduler.schedules, job)
	b.scheduler.mu.Unlock()
	// Nudge a running Start loop to re-arm its timer for this new (possibly
	// sub-minute) schedule. Non-blocking: the buffered channel coalesces
	// bursts, and a not-yet-started scheduler simply has a full/absent
	// buffer that Start drains on entry.
	if b.scheduler.wake != nil {
		select {
		case b.scheduler.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// Start begins the scheduling loop. It blocks until ctx is cancelled.
//
// Each pass sleeps until the earliest NextRun, at most a minute, so a job
// registered after Start still fires and no interval is rounded up to a
// coarser poll.
func (s *Scheduler) Start(ctx context.Context) {
	timer := time.NewTimer(s.nextWake(time.Now()))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			// A job was registered; dispatch anything already due and
			// re-arm for the (possibly earlier) next run.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
		now := time.Now()
		s.dispatchDue(ctx, now)
		timer.Reset(s.nextWake(now))
	}
}

// maxSchedulerWake caps one sleep of Start, and is the cadence of an
// Every(0) schedule; minSchedulerWake keeps a due-now schedule from spinning.
const (
	maxSchedulerWake = time.Minute
	minSchedulerWake = time.Millisecond
)

// nextWake is how long Start sleeps after a pass at now: until the earliest
// NextRun, between minSchedulerWake and maxSchedulerWake.
func (s *Scheduler) nextWake(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	wake := maxSchedulerWake
	for _, sch := range s.schedules {
		if sch.NextRun.IsZero() || (sch.cron == nil && sch.Interval <= 0) {
			continue // a stopped cron schedule, or Every(0) on the minute cap
		}
		wake = min(wake, sch.NextRun.Sub(now))
	}
	return max(wake, minSchedulerWake)
}

func (s *Scheduler) dispatchDue(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.schedules {
		sch := &s.schedules[i]
		if sch.cron != nil && sch.NextRun.IsZero() {
			continue // stopped: its re-arm found no next firing (below)
		}
		if now.After(sch.NextRun) || now.Equal(sch.NextRun) {
			job := Job{
				Type:        sch.Type,
				Payload:     sch.Payload,
				Lane:        sch.Lane,
				Priority:    sch.Priority,
				MaxAttempts: sch.MaxAttempts,
				UserID:      sch.UserID,
				CreatedAt:   now,
			}
			// Preserve the pre-options contract: a scheduled job always
			// reaches Enqueue with a non-zero MaxAttempts so custom Queue
			// implementations (which may not apply the default themselves)
			// see the same value MemoryQueue/DBQueue would have produced.
			if job.MaxAttempts == 0 {
				job.MaxAttempts = 3
			}

			for _, q := range s.queues {
				if err := q.Enqueue(ctx, job); err != nil {
					s.logger.Error("scheduler: enqueue failed",
						"job_type", sch.Type,
						"err", err,
					)
				}
			}

			if sch.cron != nil {
				sch.NextRun = nextFire(*sch.cron, sch.loc, now)
				if sch.NextRun.IsZero() {
					// Registration refuses a spec that never fires, so
					// this guards Next alone: a zero NextRun would fire
					// on every tick, and the schedule stops instead.
					s.logger.Warn("scheduler: cron schedule stopped, no next firing",
						"job_type", sch.Type,
					)
				}
			} else {
				sch.NextRun = nextIntervalRun(sch.NextRun, sch.Interval, now)
			}
		}
	}
}

// nextIntervalRun is the first run after now on the grid NextRun set: the
// schedule keeps its phase, and runs missed while the process slept are
// skipped, not replayed. A zero interval is due again at once.
func nextIntervalRun(prev time.Time, interval time.Duration, now time.Time) time.Time {
	if interval <= 0 {
		return now
	}
	next := prev.Add(interval)
	if !next.After(now) {
		next = next.Add(now.Sub(next)/interval*interval + interval)
	}
	return next
}
