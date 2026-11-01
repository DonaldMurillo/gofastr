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
)

// MaxJobNameBytes caps the length of a job name. The cap exists to keep
// names cheap to log; it is not a security boundary on the name itself
// (names are opaque labels, sanitise them at any rendering call site).
const MaxJobNameBytes = 256

// ErrInvalidJobName is returned by Register when Name is empty or longer
// than MaxJobNameBytes.
var ErrInvalidJobName = errors.New("cron: invalid job name")

// ErrNilJobRun is returned by Register when Run is nil. A nil Run would
// nil-pointer at execution time; rejecting at registration surfaces the
// bug at startup instead of inside a tick callback.
var ErrNilJobRun = errors.New("cron: job Run is nil")

// CronJob is the unit of scheduled work.
//
// Run is invoked on every firing, its context is derived from the
// scheduler's parent context, so cancelling the scheduler cancels in-flight
// runs at the next yield point.
//
// If Run returns an error it is forwarded to the scheduler's OnError
// callback (if set); otherwise it is logged via slog.Default(), jobs
// should not crash the process.
type CronJob struct {
	Name string
	Spec string
	Run  func(ctx context.Context) error
}

// LeaderElection gates a Scheduler so only one replica fires jobs each tick,
// making cron safe to run across multiple replicas. Install one via
// [Scheduler.WithLeaderElection]; when set, the tick loop acquires the lease
// before firing and releases it after that tick's jobs finish, so a
// non-leader replica skips the tick. A lease that also implements
// [TickLeaderElection] is released as soon as its claim is made instead.
// The built-in [PostgresAdvisoryLease] uses a Postgres advisory lock; a
// custom implementation can back it with Redis, etcd, etc.
//
// Acquire alone is mutual exclusion while the lease is held: a replica whose
// timer fires for the same minute after the leader released it can fire
// that minute again. A lease that also implements [TickLeaderElection]
// (PostgresAdvisoryLease does) records the claimed minute, so each tick
// fires at most once across replicas. Neither is exactly-once execution; for
// durable, exactly-once background work use the DB-backed queue
// (battery/queue.DurableScheduler).
type LeaderElection interface {
	// Acquire attempts to take the lease for this tick. held is true when the
	// caller is the leader and should fire jobs; in that case release (non-nil)
	// relinquishes it. A non-leader returns (false, nil, nil). Acquire must not
	// block indefinitely, a non-leader should return promptly so the replica
	// can skip to the next tick.
	Acquire(ctx context.Context) (held bool, release func(), err error)
}

// TickLeaderElection is an optional [LeaderElection] capability that makes
// each tick fire at most once across replicas. Plain Acquire gives mutual
// exclusion only while the lease is held, and the scheduler releases it as
// soon as the tick's jobs finish: a replica whose timer fires for the same
// minute a moment later finds the lease free and runs the jobs again.
// AcquireTick closes that gap by recording which minute was consumed.
//
// Because the claim is the guarantee, the scheduler releases a tick lease
// as soon as AcquireTick holds, before the tick's jobs start: a long job
// does not hold the lease, and the next minute fires on schedule (on any
// replica, this one included) while it runs.
//
// When the installed lease implements it, the scheduler calls AcquireTick
// instead of Acquire, passing the tick minute (the firing time truncated to
// the minute, in UTC). An implementation must hold only when it took the
// lease AND no replica sharing the lease has claimed this minute or a later
// one, and it must record the claim atomically (a conditional write, not a
// read followed by a write). [PostgresAdvisoryLease] implements it with a
// small table and a conditional upsert.
//
// Limits: the guarantee is at most once, not exactly once. A tick whose
// claim fails or loses is skipped, never retried, and a replica that claims
// and then crashes leaves the minute unrun. Claims are monotonic, so a
// replica whose clock runs behind cannot fire a minute older than one
// already claimed, and a replica whose clock runs ahead fires its minute
// early by its skew; skew of a minute or more skips minutes. Every replica
// sharing the lease must register the same jobs, since one claim covers
// the whole tick.
type TickLeaderElection interface {
	LeaderElection
	// AcquireTick attempts to take the lease and claim tick. held, release
	// and err follow Acquire's contract; a lease taken for a minute that is
	// already claimed is released before returning (false, nil, nil).
	AcquireTick(ctx context.Context, tick time.Time) (held bool, release func(), err error)
}

// Scheduler is a tiny in-process cron driver. It is intentionally minimal:
// no persistence, and by default no overlap protection across replicas (every
// replica fires every tick, fine for a single instance). For horizontally
// scaled deployments, install a [LeaderElection] via WithLeaderElection so
// only one replica fires each tick, or use the DB-backed queue
// (battery/queue.DurableScheduler) for durable, exactly-once work.
type Scheduler struct {
	mu        sync.RWMutex
	jobs      []scheduledJob
	tickEv    time.Duration
	stop      chan struct{}
	stopped   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once      // Stop/StopContext may race (drainer vs. user hook); only one may close stop
	started   bool           // set under mu inside Start; gates Stop's wait on stopped
	inflight  sync.WaitGroup // one Add per job goroutine; Stop/StopContext join it
	OnError   func(jobName string, err error)

	// gate, when set, is checked in RunOnce for every firing job before
	// inflight.Add. Returning false skips the job for this tick.
	// Framework code uses it to skip jobs owned by a disabled module.
	gate func(jobName string) bool

	// leader, when set, gates the whole tick to the lease holder so cron is
	// safe across replicas. nil = every replica fires (single-instance mode).
	leader LeaderElection
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

// Register adds a job. Returns an error if the spec is invalid, the name
// is empty or oversize, or Run is nil, callers catch typos at registration
// time rather than silently failing forever or nil-pointering at firing.
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

// SetGate installs a gate checked in RunOnce for every firing job. When
// gate returns false the job is skipped for this tick. Framework code
// uses it to skip jobs owned by a disabled module. Pass nil to clear.
func (s *Scheduler) SetGate(gate func(jobName string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = gate
}

// WithLeaderElection installs a leader-election gate so the scheduler fires
// jobs only when it holds the lease, making cron safe across replicas. With
// none set (the default) every replica fires every tick. Build a lease with
// [NewPostgresAdvisoryLease] or supply a custom [LeaderElection].
func (s *Scheduler) WithLeaderElection(le LeaderElection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leader = le
}

// runTick is one tick: acquire leadership (if configured), fire the matching
// jobs, and release the lease. It reports whether this replica fired.
//
// A lease that implements [TickLeaderElection] claims the tick minute, and
// the claim alone keeps the minute at most once across replicas, so the
// lease is released as soon as AcquireTick holds, before the jobs start.
// A plain lease is mutual exclusion only while held, so it is held until
// THIS tick's jobs finish, never a job from an earlier tick. Waiting on the
// scheduler-wide inflight group instead held the lease through any long or
// hung job, and with a session-scoped lock (Postgres) the leader's own
// next tick could not take it either: every tick of every job skipped,
// cluster-wide, until that one job ended. Extracted from the run loop so
// the leader-gating is testable without the wall clock.
func (s *Scheduler) runTick(ctx context.Context, now time.Time) bool {
	s.mu.RLock()
	leader := s.leader
	s.mu.RUnlock()
	if leader == nil {
		s.RunOnce(ctx, now)
		return true
	}
	acquire := leader.Acquire
	tl, claims := leader.(TickLeaderElection)
	if claims {
		tick := now.UTC().Truncate(time.Minute)
		acquire = func(ctx context.Context) (bool, func(), error) { return tl.AcquireTick(ctx, tick) }
	}
	held, release, err := s.acquireSafely(ctx, acquire)
	if err != nil {
		s.reportError("(leader-election)", err)
		return false
	}
	if !held {
		return false
	}
	if claims {
		// Released off the run loop: release is driver code (a DB round
		// trip for Postgres) and the loop must stay selectable on s.stop /
		// ctx.Done() so StopContext's deadline holds.
		go s.releaseSafely(release)
		s.RunOnce(ctx, now)
		return true
	}
	// Hold the plain lease until this tick's jobs finish so a peer replica
	// can't also fire this tick, but in a background goroutine, NOT inline:
	// the run loop must stay selectable on s.stop / ctx.Done() so a job that
	// ignores its context cannot pin the loop and defeat StopContext's
	// deadline-bounded join. Registered BEFORE runOnce so a panic inside it
	// (e.g. a panicking gate) still schedules the release instead of leaking
	// the lease.
	var tickJobs sync.WaitGroup
	defer func() {
		go func() {
			tickJobs.Wait()
			s.releaseSafely(release)
		}()
	}()
	s.runOnce(ctx, now, &tickJobs)
	return true
}

// acquireSafely runs the leader-election Acquire (or AcquireTick) under
// the same recover net the per-job path gives j.Run: it is driver-supplied
// callback code (custom Redis/etcd leases are a documented extension point)
// running on the loop goroutine, which has no per-request net — its
// panic is routed to OnError like every other scheduler fault and the
// tick is skipped, instead of killing the process.
func (s *Scheduler) acquireSafely(ctx context.Context, acquire func(context.Context) (bool, func(), error)) (held bool, release func(), err error) {
	defer func() {
		if r := recover(); r != nil {
			held, release = false, nil
			err = fmt.Errorf("panic in leader-election acquire: %v\n%s", r, debug.Stack())
		}
	}()
	return acquire(ctx)
}

// releaseSafely runs the per-tick lease release under a recover guard
// and routes the wrapped panic to OnError: release wraps driver teardown
// (e.g. closing a pinned conn) and runs on an untracked goroutine,
// where an escape is process-fatal.
func (s *Scheduler) releaseSafely(release func()) {
	defer func() {
		if r := recover(); r != nil {
			s.reportError("(leader-election)", fmt.Errorf("panic in leader-election release: %v\n%s", r, debug.Stack()))
		}
	}()
	release()
}

// Start begins the tick loop in a goroutine. Returns immediately. Idempotent:
// repeated Start calls are no-ops once the loop is running.
func (s *Scheduler) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		go s.run(ctx)
	})
}

// Stop signals the loop to exit and blocks until it has, then joins any
// in-flight job goroutines, a job mid-write must finish before graceful
// shutdown lets the process exit. Safe to call multiple times, and safe
// to call before Start, if the loop was never launched there is nothing
// to wait for, so Stop returns immediately instead of blocking forever
// on a channel the loop never closes (which would hang graceful shutdown
// when boot aborts before Start runs).
//
// Stop waits for in-flight jobs without a deadline; use StopContext when
// the join must be bounded (App wires its cron drain through StopContext
// so a job that ignores its context cannot hang SIGTERM forever).
func (s *Scheduler) Stop() {
	_ = s.StopContext(context.Background())
}

// StopContext is Stop with a deadline on the in-flight join: it signals
// the loop to exit, waits for it, then waits for running job goroutines
// until ctx expires. Jobs receive the scheduler's parent context, which
// App.Shutdown cancels before draining, so well-behaved jobs exit
// promptly; a job that ignores its context is abandoned at the deadline
// and ctx.Err() is returned.
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

// RunOnce fires every job whose schedule matches the given minute. Exported
// for tests that drive the tick manually instead of waiting on the wall clock;
// production code lets the loop call this.
//
// Iterates under the lock without copying the slice. Jobs that mutate state
// (Register during tick) are safe because the mutex is held only for the
// read, new jobs appear on the next tick.
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) {
	s.runOnce(ctx, now, nil)
}

// runOnce is RunOnce that also counts each job it starts into tickJobs
// when it is non-nil, so a plain lease can be held for this tick's jobs
// alone.
func (s *Scheduler) runOnce(ctx context.Context, now time.Time, tickJobs *sync.WaitGroup) {
	// Snapshot the firing jobs AND the gate under the read lock, then
	// evaluate the gate OUTSIDE it. Gates are registered callbacks
	// (SetGate) free to do I/O, and a blocking gate holding s.mu
	// stalls Register, NextRun and every other RunOnce — the same
	// registry rule core/mcp's listing gates learned (b79942f7).
	// Registered jobs are immutable values, so a snapshot is exact.
	// The gate is snapshotted too (not re-read in gateAllow): SetGate
	// may swap or clear it concurrently, and a torn nil-check/invoke
	// pair would call a gate that is already gone.
	s.mu.RLock()
	var firing []CronJob
	var gate func(jobName string) bool
	for i := range s.jobs {
		sj := &s.jobs[i]
		if sj.expr.matches(now) {
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
		}
		go func(j CronJob) {
			defer s.inflight.Done()
			if tickJobs != nil {
				defer tickJobs.Done()
			}
			defer func() {
				if r := recover(); r != nil {
					s.reportError(j.Name, fmt.Errorf("panic: %v\n%s", r, debug.Stack()))
				}
			}()
			if err := j.Run(ctx); err != nil {
				s.reportError(j.Name, err)
			}
		}(job)
	}
}

// gateAllow evaluates the snapshot gate with the same net the job run
// gets above: a panicking gate reports and denies the job instead of
// killing the scheduler goroutine (recovercallback pins this —
// RunOnce sits on the ticker-driven dispatch loop).
func (s *Scheduler) gateAllow(gate func(jobName string) bool, name string) (allow bool) {
	defer func() {
		if r := recover(); r != nil {
			s.reportError(name, fmt.Errorf("panic in gate: %v\n%s", r, debug.Stack()))
			allow = false
		}
	}()
	return gate(name)
}

// reportError forwards a job failure to OnError under its own recover
// guard: OnError is app-supplied callback code running on the scheduler
// loop, which has no per-request net — a panicking error handler must
// not take the scheduler (and the process) down with it. With no
// OnError configured the failure still lands somewhere observable: the
// scheduler loop has no caller to hand it to, so it goes to
// slog.Default() (a recovered gate panic used to drop the panic value
// and stack on the floor while the job silently skipped).
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

// untilNextTick is how long until the next tickEv boundary.
func (s *Scheduler) untilNextTick() time.Duration {
	now := time.Now()
	return now.Truncate(s.tickEv).Add(s.tickEv).Sub(now)
}

func (s *Scheduler) run(ctx context.Context) {
	defer close(s.stopped)

	// Every tick, the first included, fires at the next interval boundary
	// (:00 of a minute): rearming one interval after a slow tick finished
	// let the delay pile up until a whole minute was skipped.
	timer := time.NewTimer(s.untilNextTick())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case t := <-timer.C:
			s.runTick(ctx, t)
			timer.Reset(s.untilNextTick())
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
	domAny bool   // day-of-month field begins with wildcard syntax
	dowAny bool   // day-of-week field begins with wildcard syntax

	minuteAny bool // minute field begins with wildcard syntax
	hourAny   bool // hour field begins with wildcard syntax
}

func (e cronExpr) matches(t time.Time) bool {
	return e.minute&(1<<uint(t.Minute())) != 0 &&
		e.hour&(1<<uint(t.Hour())) != 0 &&
		e.matchesDay(t)
}

// matchesDay reports whether t's date passes the month and day fields.
func (e cronExpr) matchesDay(t time.Time) bool {
	domMatch := e.dom&(1<<uint(t.Day())) != 0
	dowMatch := e.dow&(1<<uint(t.Weekday())) != 0
	dayMatch := domMatch && dowMatch
	if !e.domAny && !e.dowAny {
		// Standard cron runs when either restricted day field matches.
		dayMatch = domMatch || dowMatch
	}
	return dayMatch && e.month&(1<<uint(t.Month())) != 0
}

// satisfiable reports whether some date passes the month and day fields.
// With both day fields restricted either one matching is enough, and every
// month holds every weekday. Otherwise the date must exist (Feb 29 does,
// Feb 30 does not), and every date meets every weekday within one 400-year
// Gregorian cycle.
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
	e.minuteAny = strings.HasPrefix(fields[0], "*")
	e.hourAny = strings.HasPrefix(fields[1], "*")
	e.domAny = strings.HasPrefix(fields[2], "*")
	e.dowAny = strings.HasPrefix(fields[4], "*")
	return e, nil
}

// parseField parses a single cron field into a bitmask over [min,max].
// Supports comma-separated lists, ranges (a-b), and step values (*/N or a-b/N).
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
		// Compare before adding: an otherwise valid but very large step
		// can overflow int, wrap below hi, and set unrelated schedule bits.
		if step > hi-i {
			break
		}
		i += step
	}
	return mask, nil
}
