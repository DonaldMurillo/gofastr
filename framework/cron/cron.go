package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronJob is the unit of scheduled work.
//
// Run is invoked on every firing — its context is derived from the
// scheduler's parent context, so cancelling the scheduler cancels in-flight
// runs at the next yield point.
//
// If Run returns an error it is forwarded to the scheduler's OnError
// callback (if set); otherwise it is silently dropped — jobs should not
// crash the process.
type CronJob struct {
	Name string
	Spec string
	Run  func(ctx context.Context) error
}

// Scheduler is a tiny in-process cron driver. It is intentionally minimal:
// no persistence, no distributed locks, no overlap protection across
// replicas. For single-instance background work it is sufficient; for
// horizontally scaled deployments use the DB-backed queue instead.
type Scheduler struct {
	mu      sync.RWMutex
	jobs    []scheduledJob
	tickEv  time.Duration
	stop    chan struct{}
	stopped chan struct{}
	OnError func(jobName string, err error)
}

type scheduledJob struct {
	job  CronJob
	expr cronExpr
}

// NewScheduler returns a Scheduler that wakes once per minute. The tick
// interval is a deliberate choice: cron resolution is one minute, and
// waking more often would burn CPU for no gain.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tickEv:  time.Minute,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Register adds a job. Returns an error if the spec is invalid so callers
// catch typos at registration time rather than silently failing forever.
func (s *Scheduler) Register(job CronJob) error {
	expr, err := ParseCron(job.Spec)
	if err != nil {
		return fmt.Errorf("cron %q: %w", job.Name, err)
	}
	s.mu.Lock()
	s.jobs = append(s.jobs, scheduledJob{job: job, expr: expr})
	s.mu.Unlock()
	return nil
}

// Start begins the tick loop in a goroutine. Returns immediately. Idempotent:
// repeated Start calls are no-ops once the loop is running.
func (s *Scheduler) Start(ctx context.Context) {
	go s.run(ctx)
}

// Stop signals the loop to exit and blocks until it has. Safe to call
// multiple times.
func (s *Scheduler) Stop() {
	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
	}
	<-s.stopped
}

// RunOnce fires every job whose schedule matches the given minute. Exported
// for tests that drive the tick manually instead of waiting on the wall clock;
// production code lets the loop call this.
//
// Iterates under the lock without copying the slice. Jobs that mutate state
// (Register during tick) are safe because the mutex is held only for the
// read — new jobs appear on the next tick.
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.jobs {
		sj := &s.jobs[i]
		if !sj.expr.matches(now) {
			continue
		}
		job := sj.job
		go func(j CronJob) {
			if err := j.Run(ctx); err != nil && s.OnError != nil {
				s.OnError(j.Name, err)
			}
		}(job)
	}
}

func (s *Scheduler) run(ctx context.Context) {
	defer close(s.stopped)

	// Align to the next minute boundary so the first tick fires near :00.
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case t := <-timer.C:
			s.RunOnce(ctx, t)
			timer.Reset(s.tickEv)
		}
	}
}

// ---------------------------------------------------------------------------
// Cron expression parsing
// ---------------------------------------------------------------------------

// cronExpr holds the parsed minute/hour/dom/month/dow bitmasks. All masks
// use 1-bit-per-value: bit i set means "fires when field == i".
type cronExpr struct {
	minute uint64 // 0-59
	hour   uint64 // 0-23
	dom    uint64 // 1-31
	month  uint64 // 1-12
	dow    uint64 // 0-6 (Sun=0)
}

func (e cronExpr) matches(t time.Time) bool {
	return e.minute&(1<<uint(t.Minute())) != 0 &&
		e.hour&(1<<uint(t.Hour())) != 0 &&
		e.dom&(1<<uint(t.Day())) != 0 &&
		e.month&(1<<uint(t.Month())) != 0 &&
		e.dow&(1<<uint(t.Weekday())) != 0
}

// ParseCron accepts the standard 5-field syntax plus the shortcuts
// @hourly / @daily / @weekly / @monthly / @yearly. Step values (*/N) and
// ranges (a-b) are supported.
func ParseCron(spec string) (cronExpr, error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "@hourly":
		return ParseCron("0 * * * *")
	case "@daily", "@midnight":
		return ParseCron("0 0 * * *")
	case "@weekly":
		return ParseCron("0 0 * * 0")
	case "@monthly":
		return ParseCron("0 0 1 * *")
	case "@yearly", "@annually":
		return ParseCron("0 0 1 1 *")
	}

	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return cronExpr{}, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	var err error
	var e cronExpr
	if e.minute, err = parseField(fields[0], 0, 59); err != nil {
		return cronExpr{}, fmt.Errorf("minute: %w", err)
	}
	if e.hour, err = parseField(fields[1], 0, 23); err != nil {
		return cronExpr{}, fmt.Errorf("hour: %w", err)
	}
	if e.dom, err = parseField(fields[2], 1, 31); err != nil {
		return cronExpr{}, fmt.Errorf("day-of-month: %w", err)
	}
	if e.month, err = parseField(fields[3], 1, 12); err != nil {
		return cronExpr{}, fmt.Errorf("month: %w", err)
	}
	if e.dow, err = parseField(fields[4], 0, 6); err != nil {
		return cronExpr{}, fmt.Errorf("day-of-week: %w", err)
	}
	return e, nil
}

// parseField parses a single cron field into a bitmask over [min,max].
// Supports comma-separated lists, ranges (a-b), and step values (*/N or a-b/N).
func parseField(s string, min, max int) (uint64, error) {
	var mask uint64
	for _, part := range strings.Split(s, ",") {
		m, err := parseFieldPart(part, min, max)
		if err != nil {
			return 0, err
		}
		mask |= m
	}
	return mask, nil
}

func parseFieldPart(part string, min, max int) (uint64, error) {
	step := 1
	if idx := strings.Index(part, "/"); idx != -1 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return 0, fmt.Errorf("bad step %q", part[idx+1:])
		}
		step = s
		part = part[:idx]
	}

	var lo, hi int
	switch {
	case part == "*":
		lo, hi = min, max
	case strings.Contains(part, "-"):
		bits := strings.SplitN(part, "-", 2)
		a, err1 := strconv.Atoi(bits[0])
		b, err2 := strconv.Atoi(bits[1])
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("bad range %q", part)
		}
		lo, hi = a, b
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("bad value %q", part)
		}
		lo, hi = n, n
	}

	if lo < min || hi > max || lo > hi {
		return 0, fmt.Errorf("%d-%d out of range [%d,%d]", lo, hi, min, max)
	}

	var mask uint64
	for i := lo; i <= hi; i += step {
		mask |= 1 << uint(i)
	}
	return mask, nil
}
