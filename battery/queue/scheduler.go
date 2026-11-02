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

// Job sets the job type and payload for the scheduled job. A payload
// json.Marshal refuses is returned by Register or RegisterAt, so the
// builder remains fluent.
func (b *ScheduleBuilder) Job(jobType string, payload any) *ScheduleBuilder {
	b.jobType = jobType
	b.payloadErr = nil
	if payload != nil {
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
				b.payloadErr = fmt.Errorf("queue: marshal schedule payload: %w", err)
				return b
			}
			b.payload = data
		}
	}
	return b
}

// Register adds the scheduled job to the scheduler, computing the first
// NextRun relative to the current wall clock. It returns an error when the
// payload given to Job cannot be encoded as JSON or a Cron schedule's spec
// is invalid, and registers nothing then.
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
		return nil
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
		job.NextRun = sc.Next(base)
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
// The loop re-reads the schedule set (under lock) on every tick, so jobs
// registered AFTER Start still fire, the natural wiring is "start
// subsystems, then register jobs", and an empty-at-Start scheduler that
// exited would silently drop everything registered later. The tick
// cadence adapts to the finest live interval each pass, so adding a
// sub-minute schedule after Start takes effect on the next tick.
func (s *Scheduler) Start(ctx context.Context) {
	timer := time.NewTimer(s.tickInterval())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			// A job was registered; dispatch anything already due and
			// re-arm to the (possibly finer) interval.
			s.dispatchDue(ctx, time.Now())
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.tickInterval())
		case now := <-timer.C:
			s.dispatchDue(ctx, now)
			timer.Reset(s.tickInterval())
		}
	}
}

// tickInterval returns the current wake cadence: the finest live schedule
// interval (cron counts as one minute), floored so an empty or cron-only
// scheduler still wakes once a minute to pick up late registrations.
func (s *Scheduler) tickInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	minInterval := time.Duration(0)
	for _, sch := range s.schedules {
		d := sch.Interval
		if sch.cron != nil {
			d = time.Minute
		}
		if d <= 0 {
			continue
		}
		if minInterval == 0 || d < minInterval {
			minInterval = d
		}
	}
	if minInterval <= 0 || minInterval > time.Minute {
		// Floor at a minute so a cron-only or not-yet-populated scheduler
		// still re-checks for newly registered jobs; cap the poll so a
		// long-interval schedule doesn't leave late registrations waiting.
		minInterval = time.Minute
	}
	return minInterval
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
				sch.NextRun = sch.cron.Next(now)
				if sch.NextRun.IsZero() {
					// Registration refuses a spec that never fires, so
					// this guards Next alone: a zero NextRun would fire
					// on every tick, and the schedule stops instead.
					s.logger.Warn("scheduler: cron schedule stopped, no next firing",
						"job_type", sch.Type,
					)
				}
			} else {
				sch.NextRun = now.Add(sch.Interval)
			}
		}
	}
}
