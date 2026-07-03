package queue

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

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

	// cron, when set, drives NextRun from a cron expression instead of
	// Interval. Nil for interval schedules (the pre-existing behaviour).
	cron *cron.Schedule
}

// Scheduler enqueues recurring jobs onto one or more Queue backends.
type Scheduler struct {
	mu        sync.Mutex
	queues    []Queue
	schedules []ScheduledJob
	logger    *slog.Logger

	// wake is signalled by Register so a running Start loop re-arms its
	// timer immediately, rather than waiting out a coarse (up to a minute)
	// poll before a newly-registered sub-minute schedule can fire.
	wake chan struct{}
}

// NewScheduler creates a new Scheduler that dispatches to the given queues.
// Enqueue errors are logged via slog.Default().
func NewScheduler(queues ...Queue) *Scheduler {
	return &Scheduler{
		queues: queues,
		logger: slog.Default(),
		wake:   make(chan struct{}, 1),
	}
}

// NewSchedulerWithLogger creates a new Scheduler with an explicit logger.
// Pass a non-nil *slog.Logger to control where enqueue-error messages are
// routed; passing nil falls back to slog.Default().
func NewSchedulerWithLogger(q Queue, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		queues: []Queue{q},
		logger: logger,
		wake:   make(chan struct{}, 1),
	}
}

// Every returns a ScheduleBuilder that fires on a fixed interval.
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
	scheduler *Scheduler
	interval  time.Duration
	cronSpec  string
	hasCron   bool
	jobType   string
	payload   json.RawMessage
}

// Job sets the job type and payload for the scheduled job.
func (b *ScheduleBuilder) Job(jobType string, payload any) *ScheduleBuilder {
	b.jobType = jobType
	if payload != nil {
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
	}
	return b
}

// Register adds the scheduled job to the scheduler, computing the first
// NextRun relative to the current wall clock. It returns an error only when a
// Cron schedule's spec is invalid; interval (Every) schedules never error, so
// existing callers that ignore the return value are unaffected.
func (b *ScheduleBuilder) Register() error {
	return b.RegisterAt(time.Now())
}

// RegisterAt is like Register but anchors the first NextRun to base instead of
// the wall clock. It exists so cron schedules can be registered deterministically
// (tests, replayed fixtures) without depending on time.Now().
func (b *ScheduleBuilder) RegisterAt(base time.Time) error {
	if b.jobType == "" {
		return nil
	}

	job := ScheduledJob{
		Type:     b.jobType,
		Payload:  b.payload,
		Interval: b.interval,
	}

	if b.hasCron {
		sc, err := cron.Parse(b.cronSpec)
		if err != nil {
			return err
		}
		job.cron = &sc
		job.NextRun = sc.Next(base)
	} else {
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
// registered AFTER Start still fire — the natural wiring is "start
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
		if now.After(sch.NextRun) || now.Equal(sch.NextRun) {
			job := Job{
				Type:        sch.Type,
				Payload:     sch.Payload,
				MaxAttempts: 3,
				CreatedAt:   now,
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
			} else {
				sch.NextRun = now.Add(sch.Interval)
			}
		}
	}
}
