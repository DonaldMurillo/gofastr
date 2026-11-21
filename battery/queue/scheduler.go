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

// ScheduledJob is a recurring job: a fixed Interval or a cron expression.
type ScheduledJob struct {
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Interval time.Duration   `json:"interval"`
	NextRun  time.Time       `json:"next_run"`

	// Lane, Priority and MaxAttempts are carried into every Job the schedule
	// fires.
	Lane        string `json:"lane,omitempty"`
	Priority    int    `json:"priority"`
	MaxAttempts int    `json:"max_attempts"`

	// UserID is carried into every Job the schedule fires.
	UserID string `json:"user_id,omitempty"`

	cron *cron.Schedule
	loc  *time.Location
}

// Scheduler is the non-durable, single-process scheduler; use DurableScheduler
// across replicas or restarts.
type Scheduler struct {
	mu        sync.Mutex
	queues    []Queue
	schedules []ScheduledJob
	logger    *slog.Logger

	wake chan struct{}

	eraseName       string
	unregisterErase func()
}

// NewScheduler creates a Scheduler that dispatches to queues.
func NewScheduler(queues ...Queue) *Scheduler {
	return newScheduler(queues, slog.Default())
}

func newScheduler(queues []Queue, logger *slog.Logger) *Scheduler {
	s := &Scheduler{
		queues: queues,
		logger: logger,
		wake:   make(chan struct{}, 1),
	}
	s.eraseName, s.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:     "queue_scheduler_memory",
		Source:   "queue",
		Erase:    s.eraseUser,
		Producer: true,
	})
	return s
}

func (s *Scheduler) eraserName() string { return s.eraseName }

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

type userJobSweeper interface {
	sweepUserJobs(ctx context.Context, userID string) (int, error)
}

// Close removes the scheduler from App.EraseUserData's reach; cancel Start's
// context to stop it.
func (s *Scheduler) Close() error {
	if s.unregisterErase != nil {
		s.unregisterErase()
	}
	return nil
}

// NewInMemoryScheduler is NewScheduler.
func NewInMemoryScheduler(queues ...Queue) *Scheduler {
	return NewScheduler(queues...)
}

// NewSchedulerWithLogger is NewScheduler with a logger; nil means
// slog.Default().
func NewSchedulerWithLogger(q Queue, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return newScheduler([]Queue{q}, logger)
}

// Every returns a ScheduleBuilder that fires on a fixed interval of at least a
// millisecond.
func (s *Scheduler) Every(interval time.Duration) *ScheduleBuilder {
	return &ScheduleBuilder{
		scheduler: s,
		interval:  interval,
	}
}

// Cron returns a ScheduleBuilder that fires on a 5-field cron expression or an
// @shortcut.
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

// Register adds the schedule, its first NextRun after the wall clock.
func (b *ScheduleBuilder) Register() error {
	return b.RegisterAt(time.Now())
}

// Lane routes every job this schedule fires to the given lane.
func (b *ScheduleBuilder) Lane(name string) *ScheduleBuilder {
	b.lane = name
	return b
}

// Priority sets the priority of every job this schedule fires.
func (b *ScheduleBuilder) Priority(p int) *ScheduleBuilder {
	b.priority = p
	return b
}

// MaxAttempts sets the retry ceiling of every job this schedule fires; zero
// means 3.
func (b *ScheduleBuilder) MaxAttempts(n int) *ScheduleBuilder {
	b.maxAttempts = n
	return b
}

// UserID names the person the schedule's payload is about.
func (b *ScheduleBuilder) UserID(id string) *ScheduleBuilder {
	b.userID = id
	return b
}

// RegisterAt is Register with the first NextRun anchored to base.
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
		job.loc = base.Location()
		job.NextRun = nextFire(sc, job.loc, base)
		if job.NextRun.IsZero() {
			return fmt.Errorf("queue: cron spec %q never fires", b.cronSpec)
		}
	} else {
		if b.interval < 0 || (b.interval > 0 && b.interval < minDurableInterval) {
			return fmt.Errorf("queue: schedule interval %s is below the %s floor", b.interval, minDurableInterval)
		}
		job.NextRun = base.Add(b.interval)
	}

	b.scheduler.mu.Lock()
	b.scheduler.schedules = append(b.scheduler.schedules, job)
	b.scheduler.mu.Unlock()
	if b.scheduler.wake != nil {
		select {
		case b.scheduler.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// Start runs the scheduling loop until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	timer := time.NewTimer(s.nextWake(time.Now()))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
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

const (
	maxSchedulerWake = time.Minute
	minSchedulerWake = time.Millisecond
)

func (s *Scheduler) nextWake(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	wake := maxSchedulerWake
	for _, sch := range s.schedules {
		if sch.NextRun.IsZero() || (sch.cron == nil && sch.Interval <= 0) {
			continue
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
			continue
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
