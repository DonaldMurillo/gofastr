package cron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/core/tracing"
)

// MaxJobNameBytes is the longest job name Register accepts.
const MaxJobNameBytes = 256

// ErrInvalidJobName is returned by Register for an empty or oversize name.
var ErrInvalidJobName = errors.New("cron: invalid job name")

// ErrNilJobRun is returned by Register when Run is nil.
var ErrNilJobRun = errors.New("cron: job Run is nil")

// CronJob is one scheduled job. Run gets the scheduler's context; its error
// goes to OnError, or to slog when OnError is nil.
type CronJob struct {
	Name string
	Spec string
	Run  func(ctx context.Context) error
}

// LeaderElection lets one replica fire each tick: the lease is held until the
// tick's jobs finish, mutual exclusion and not at-most-once per minute.
type LeaderElection interface {
	// Acquire returns held and a non-nil release for the leader, and (false, nil,
	// nil) promptly for a non-leader.
	Acquire(ctx context.Context) (held bool, release func(), err error)
}

// TickLeaderElection also records each minute it fires, so a minute fires at
// most once across replicas; it is released before the jobs start.
type TickLeaderElection interface {
	LeaderElection
	// AcquireTick holds only when it took the lease and no replica claimed tick
	// (the UTC minute) or a later one, recording the claim atomically; a refused
	// claim releases the lease and returns (false, nil, nil).
	AcquireTick(ctx context.Context, tick time.Time) (held bool, release func(), err error)
}

// Scheduler is an in-process cron driver with no persistence. Every replica
// fires every tick unless a [LeaderElection] is installed; for durable work
// use battery/queue's DurableScheduler.
type Scheduler struct {
	mu        sync.RWMutex
	jobs      []scheduledJob
	tickEv    time.Duration
	stop      chan struct{}
	stopped   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	started   bool
	inflight  sync.WaitGroup
	// OnError receives each job's error or recovered panic; nil logs them.
	OnError func(jobName string, err error)
	clock   func() time.Time

	gate func(jobName string) bool

	leader LeaderElection

	handedBack chan struct{}
}

type scheduledJob struct {
	job  CronJob
	expr cronExpr
}

// NewScheduler returns a Scheduler that ticks on each minute boundary.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tickEv:  time.Minute,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		clock:   time.Now,
	}
}

// Register adds a job, or returns ErrInvalidJobName, ErrNilJobRun or the
// spec's parse error.
func (s *Scheduler) Register(job CronJob) error {
	if job.Name == "" || len(job.Name) > MaxJobNameBytes {
		return fmt.Errorf("%w: name length %d (must be 1..%d)", ErrInvalidJobName, len(job.Name), MaxJobNameBytes)
	}
	if job.Run == nil {
		return fmt.Errorf("%w: %q", ErrNilJobRun, job.Name)
	}
	expr, err := ParseCron(job.Spec)
	if err != nil {
		return fmt.Errorf("cron %q: %w", job.Name, err)
	}
	s.mu.Lock()
	s.jobs = append(s.jobs, scheduledJob{job: job, expr: expr})
	s.mu.Unlock()
	return nil
}

// SetGate installs gate, asked before each firing; false skips that job for
// the tick. nil clears it.
func (s *Scheduler) SetGate(gate func(jobName string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = gate
}

// WithLeaderElection installs le, so the scheduler fires only while it holds
// the lease.
func (s *Scheduler) WithLeaderElection(le LeaderElection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leader = le
}

func (s *Scheduler) runTick(ctx context.Context, now time.Time) bool {
	w := s.beginWalk()
	defer w.end()
	return w.fire(ctx, now, false)
}

type walk struct {
	s      *Scheduler
	leader LeaderElection

	asked   bool
	held    bool
	release func()
	jobs    tickGroup
}

type tickGroup struct {
	sync.WaitGroup
	n int
}

func (s *Scheduler) beginWalk() *walk {
	s.mu.RLock()
	leader := s.leader
	s.mu.RUnlock()
	return &walk{s: s, leader: leader}
}

func (w *walk) fire(ctx context.Context, now time.Time, fixedOnly bool) bool {
	s := w.s
	if w.leader == nil {
		s.runOnce(ctx, now, nil, fixedOnly)
		return true
	}
	if tl, claims := w.leader.(TickLeaderElection); claims {
		if !s.awaitHandBack(ctx) {
			return false
		}
		tick := now.UTC().Truncate(time.Minute)
		held, release, err := s.acquireSafely(ctx, func(ctx context.Context) (bool, func(), error) {
			return tl.AcquireTick(ctx, tick)
		})
		if err != nil {
			s.reportError("(leader-election)", err)
			return false
		}
		if !held {
			return false
		}
		s.handBack(release)
		s.runOnce(ctx, now, nil, fixedOnly)
		return true
	}
	if !w.asked {
		w.asked = true
		if !s.awaitHandBack(ctx) {
			return false
		}
		held, release, err := s.acquireSafely(ctx, w.leader.Acquire)
		if err != nil {
			w.asked = false
			s.reportError("(leader-election)", err)
			return false
		}
		w.held, w.release = held, release
	}
	if !w.held {
		return false
	}
	s.runOnce(ctx, now, &w.jobs, fixedOnly)
	return true
}

func (w *walk) end() {
	if !w.held {
		return
	}
	w.held = false
	if w.jobs.n == 0 {
		w.s.handBack(w.release)
		return
	}
	go func() {
		w.jobs.Wait()
		w.s.releaseSafely(w.release)
	}()
}

func (s *Scheduler) handBack(release func()) {
	done := make(chan struct{})
	s.mu.Lock()
	s.handedBack = done
	s.mu.Unlock()
	go func() {
		defer close(done)
		s.releaseSafely(release)
	}()
}

func (s *Scheduler) awaitHandBack(ctx context.Context) bool {
	s.mu.RLock()
	done := s.handedBack
	s.mu.RUnlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-s.stop:
		return false
	}
}

func (s *Scheduler) acquireSafely(ctx context.Context, acquire func(context.Context) (bool, func(), error)) (held bool, release func(), err error) {
	defer func() {
		if r := recover(); r != nil {
			held, release = false, nil
			err = fmt.Errorf("panic in leader-election acquire: %v\n%s", r, debug.Stack())
		}
	}()
	return acquire(ctx)
}

func (s *Scheduler) releaseSafely(release func()) {
	defer func() {
		if r := recover(); r != nil {
			s.reportError("(leader-election)", fmt.Errorf("panic in leader-election release: %s\n%s", textsafe.Recovered(r), debug.Stack()))
		}
	}()
	release()
}

// Start runs the tick loop on a goroutine; later calls do nothing.
func (s *Scheduler) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		go s.run(ctx)
	})
}

// Stop is StopContext with no deadline.
func (s *Scheduler) Stop() {
	_ = s.StopContext(context.Background())
}

// StopContext stops the loop and waits for it and for running jobs, returning
// ctx's error if the deadline comes first. It is safe before Start and more
// than once.
func (s *Scheduler) StopContext(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.RLock()
	started := s.started
	s.mu.RUnlock()
	if started {
		select {
		case <-s.stopped:
		case <-ctx.Done():
			return fmt.Errorf("cron: tick loop still running at stop deadline: %w", ctx.Err())
		}
	}

	joined := make(chan struct{})
	go func() {
		s.inflight.Wait()
		close(joined)
	}()
	select {
	case <-joined:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cron: in-flight jobs still running at stop deadline: %w", ctx.Err())
	}
}

// RunOnce fires every job that fires at now's minute, with
// [Schedule.FiresAt]'s daylight-saving handling, as one tick of the loop does.
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) {
	s.runOnce(ctx, now, nil, false)
}

func (s *Scheduler) runOnce(ctx context.Context, now time.Time, tickJobs *tickGroup, fixedOnly bool) {
	s.mu.RLock()
	var firing []CronJob
	var gate func(jobName string) bool
	for i := range s.jobs {
		sj := &s.jobs[i]
		sched := Schedule{expr: sj.expr}
		if fixedOnly && !sched.FixedTime() {
			continue
		}
		if sched.FiresAt(now.Add(-time.Minute), now) {
			firing = append(firing, sj.job)
		}
	}
	gate = s.gate
	s.mu.RUnlock()

	for _, job := range firing {
		if gate != nil && !s.gateAllow(gate, job.Name) {
			continue
		}
		s.inflight.Add(1)
		if tickJobs != nil {
			tickJobs.Add(1)
			tickJobs.n++
		}
		go func(j CronJob) {
			defer s.inflight.Done()
			if tickJobs != nil {
				defer tickJobs.Done()
			}
			defer func() {
				if r := recover(); r != nil {
					s.reportError(j.Name, fmt.Errorf("panic: %s\n%s", textsafe.Recovered(r), debug.Stack()))
				}
			}()
			if err := tracing.Job(ctx, "cron "+j.Name, j.Run); err != nil {
				s.reportError(j.Name, err)
			}
		}(job)
	}
}

func (s *Scheduler) gateAllow(gate func(jobName string) bool, name string) (allow bool) {
	defer func() {
		if r := recover(); r != nil {
			s.reportError(name, fmt.Errorf("panic in gate: %s\n%s", textsafe.Recovered(r), debug.Stack()))
			allow = false
		}
	}()
	return gate(name)
}

func (s *Scheduler) reportError(jobName string, err error) {
	if s.OnError == nil {
		slog.Default().Error("cron: job failed", "job", jobName, "err", err)
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("cron: OnError panicked", "job", jobName, "panic", textsafe.Recovered(rec), "err", err)
		}
	}()
	s.OnError(jobName, err)
}

func (s *Scheduler) run(ctx context.Context) {
	defer close(s.stopped)

	timer := time.NewTimer(0)
	defer timer.Stop()
	arm := func() (target time.Time) {
		now := s.clock()
		target = now.Truncate(s.tickEv).Add(s.tickEv)
		wait := target.Sub(s.clock())
		if wait < 0 {
			wait = 0
		}
		timer.Reset(wait)
		return target
	}

	last := s.clock().Truncate(s.tickEv)
	target := arm()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-timer.C:
			last = s.catchUp(ctx, last, target, s.clock())
			target = arm()
		}
	}
}

const maxCatchUp = 2 * time.Hour

func (s *Scheduler) catchUp(ctx context.Context, last, tick, now time.Time) time.Time {
	end := tick
	if now.Sub(tick) >= s.tickEv {
		end = now.Truncate(s.tickEv)
	}
	start := last
	if end.Sub(start) > maxCatchUp {
		start = end.Add(-maxCatchUp)
	}
	w := s.beginWalk()
	defer w.end()
	for m := start.Add(s.tickEv); !m.After(end); m = m.Add(s.tickEv) {
		select {
		case <-ctx.Done():
			return last
		case <-s.stop:
			return last
		default:
		}
		w.fire(ctx, m, m.Before(end))
		last = m
	}
	return end
}

type cronExpr struct {
	minute uint64
	hour   uint64
	dom    uint64
	month  uint64
	dow    uint64
	domAny bool
	dowAny bool

	minuteAny bool
	hourAny   bool
}

func (e cronExpr) matches(t time.Time) bool {
	return e.minute&(1<<uint(t.Minute())) != 0 &&
		e.hour&(1<<uint(t.Hour())) != 0 &&
		e.matchesDay(t)
}

func (e cronExpr) matchesDay(t time.Time) bool {
	domMatch := e.dom&(1<<uint(t.Day())) != 0
	dowMatch := e.dow&(1<<uint(t.Weekday())) != 0
	dayMatch := domMatch && dowMatch
	if !e.domAny && !e.dowAny {
		dayMatch = domMatch || dowMatch
	}
	return dayMatch && e.month&(1<<uint(t.Month())) != 0
}

func (e cronExpr) satisfiable() bool {
	if e.month == 0 || e.dow == 0 {
		return false
	}
	if !e.domAny && !e.dowAny {
		return true
	}
	for m := 1; m <= 12; m++ {
		if e.month&(1<<uint(m)) == 0 {
			continue
		}
		last := time.Date(2024, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		if e.dom&(uint64(1)<<uint(last+1)-1) != 0 {
			return true
		}
	}
	return false
}

// ParseCron parses a five-field spec, with lists, ranges and steps, or one of
// @hourly, @daily, @midnight, @weekly, @monthly, @yearly and @annually.
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
	e.minuteAny = strings.HasPrefix(fields[0], "*")
	e.hourAny = strings.HasPrefix(fields[1], "*")
	e.domAny = strings.HasPrefix(fields[2], "*")
	e.dowAny = strings.HasPrefix(fields[4], "*")
	return e, nil
}

func parseField(s string, min, max int) (uint64, error) {
	var mask uint64
	for part := range strings.SplitSeq(s, ",") {
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
	stepped := strings.Contains(part, "/")
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
		if stepped {
			hi = max
		}
	}

	if lo < min || hi > max || lo > hi {
		return 0, fmt.Errorf("%d-%d out of range [%d,%d]", lo, hi, min, max)
	}

	var mask uint64
	for i := lo; i <= hi; {
		mask |= 1 << uint(i)
		if step > hi-i {
			break
		}
		i += step
	}
	return mask, nil
}
