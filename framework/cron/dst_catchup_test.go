package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The live tick loop fires the minute of each tick with a one-minute
// look-back. A tick that arrives minutes late (sleep, GC) skipped every
// minute in between, so a fixed-time job whose minute fell inside the
// gap never fired at all — most visibly across a DST transition, where
// the gap swallows the repeated or skipped hour (#656). The loop now
// walks each missed minute, oldest first, bounded to two hours.

// fakeClock is the loop's injectable clock.
type fakeClock struct {
	mu  atomic.Value
	set atomic.Bool
}

func (c *fakeClock) Set(t time.Time) { c.mu.Store(t); c.set.Store(true) }
func (c *fakeClock) Now() time.Time {
	if !c.set.Load() {
		return time.Now()
	}
	return c.mu.Load().(time.Time)
}

// startLateScheduler registers a fixed-time job, starts the scheduler
// under clock c set to 100ms before at, then jumps the clock to
// at+jump. The first tick fires within ~100ms of real time, lands
// `jump` late, and must catch up every minute the jump skipped.
func startLateScheduler(t *testing.T, at time.Time, jump time.Duration, spec string) (*Scheduler, *fakeClock, chan struct{}, func()) {
	t.Helper()
	s := NewScheduler()
	clock := &fakeClock{}
	clock.Set(at.Add(-100 * time.Millisecond))
	armed := make(chan struct{})
	var clockCalls atomic.Int32
	s.clock = func() time.Time {
		now := clock.Now()
		if clockCalls.Add(1) == 3 {
			close(armed)
		}
		return now
	}
	fired := make(chan struct{}, 32)
	if err := s.Register(CronJob{Name: "job", Spec: spec, Run: func(context.Context) error {
		fired <- struct{}{}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	select {
	case <-armed:
	case <-time.After(5 * time.Second):
		cancel()
		s.Stop()
		t.Fatal("tick timer was not armed")
	}
	clock.Set(at.Add(jump))
	return s, clock, fired, func() { cancel(); s.Stop() }
}

// waitForFires collects n fires or fails after d.
func waitForFires(t *testing.T, fired chan struct{}, n int, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for range n {
		select {
		case <-fired:
		case <-deadline:
			t.Fatalf("timed out waiting for %d fires", n)
		}
	}
}

// A fixed-time job at 01:45 fires once across a late tick that spans
// the whole repeated hour: once in the first pass, suppressed in the
// second.
func TestTickLoopCatchesUpAcrossFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata for America/New_York: %v", err)
	}
	// 2026-11-01: 02:00 EDT falls back to 01:00 EST.
	start := time.Date(2026, 11, 1, 1, 30, 0, 0, loc)
	if _, off := start.Zone(); off != -4*3600 {
		t.Fatalf("test premise: %s is not EDT (offset %d)", start, off)
	}
	_, _, fired, stop := startLateScheduler(t, start, 85*time.Minute, "45 1 1 11 *")
	defer stop()

	// The walk covers 01:31–01:59 EDT (first pass) and 01:00–01:55 EST
	// (second pass): 01:45 fires exactly once.
	waitForFires(t, fired, 1, 5*time.Second)
	time.Sleep(300 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("the job fired twice; the second pass must be suppressed")
	default:
	}
}

// A fixed-time job at 02:30, in the hour a spring-forward skipped,
// fires once when the late tick lands five minutes past the jump.
func TestTickLoopCatchesUpAcrossSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata for America/New_York: %v", err)
	}
	// 2027-03-14: 02:00 EST springs forward to 03:00 EDT.
	start := time.Date(2027, 3, 14, 1, 45, 0, 0, loc)
	if _, off := start.Zone(); off != -5*3600 {
		t.Fatalf("test premise: %s is not EST (offset %d)", start, off)
	}
	_, _, fired, stop := startLateScheduler(t, start, 100*time.Minute, "30 2 14 3 *")
	defer stop()

	// 01:45 EST + 100min = 04:25 EDT, well past the jump. With
	// only the one-minute look-back the gap is invisible and the 02:30
	// job never fires; the walk's 03:00 boundary sees it.
	waitForFires(t, fired, 1, 5*time.Second)
	select {
	case <-fired:
		t.Fatal("the skipped-minute job fired twice")
	default:
	}
}

// A plain every-minute job fires at most once per walked minute, and a
// jump longer than the bound walks only the bounded tail.
func TestTickCatchUpIsBounded(t *testing.T) {
	s := NewScheduler()
	fired := make(chan struct{}, 256)
	if err := s.Register(CronJob{Name: "every", Spec: "* * * * *", Run: func(context.Context) error {
		fired <- struct{}{}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	now := last.Add(3 * time.Hour)
	s.catchUp(context.Background(), last, last, now)

	// The walk is bounded to two hours of minutes; the third hour is
	// dropped, not queued.
	deadline := time.After(5 * time.Second)
	got := 0
	for got < 120 {
		select {
		case <-fired:
			got++
		case <-deadline:
			t.Fatalf("timed out after %d fires (want 120)", got)
		}
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case <-fired:
		t.Fatalf("catch-up fired more than 120 minutes for a 3h jump (got at least %d)", got+1)
	default:
	}
}

// A tick that arrives between minute boundaries fires each covered
// minute exactly once: an unaligned final stamp would fire the current
// minute again on top of the walked boundary, and without tick-claim
// leader election nothing dedupes it.
func TestTickCatchUpFiresEachMinuteOnce(t *testing.T) {
	s := NewScheduler()
	fired := make(chan struct{}, 16)
	if err := s.Register(CronJob{Name: "every", Spec: "* * * * *", Run: func(context.Context) error {
		fired <- struct{}{}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	tick := last.Add(time.Minute)
	now := tick.Add(90 * time.Second) // 12:02:30, between boundaries
	s.catchUp(context.Background(), last, tick, now)

	// 12:01 and 12:02, once each.
	deadline := time.After(5 * time.Second)
	for range 2 {
		select {
		case <-fired:
		case <-deadline:
			t.Fatal("timed out waiting for the two covered minutes")
		}
	}
	time.Sleep(200 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("a minute fired twice")
	default:
	}
}

func TestTickCatchUpIncludesCurrentBoundary(t *testing.T) {
	s := NewScheduler()
	var ticks []time.Time
	s.WithLeaderElection(&recordingTickLease{ticks: &ticks})
	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	tick := last.Add(time.Minute)
	now := tick.Add(time.Minute)
	if got := s.catchUp(context.Background(), last, tick, now); !got.Equal(now) {
		t.Fatalf("last boundary = %v, want %v; claimed %v", got, now, ticks)
	}
	if len(ticks) != 2 || !ticks[0].Equal(tick) || !ticks[1].Equal(now) {
		t.Fatalf("claimed minutes = %v, want %v and %v", ticks, tick, now)
	}
}

type recordingTickLease struct {
	ticks *[]time.Time
}

func (l *recordingTickLease) Acquire(context.Context) (bool, func(), error) {
	return false, func() {}, nil
}

func (l *recordingTickLease) AcquireTick(_ context.Context, tick time.Time) (bool, func(), error) {
	*l.ticks = append(*l.ticks, tick)
	return false, func() {}, nil
}

func TestTickCatchUpStopsWhenCancelled(t *testing.T) {
	s := NewScheduler()
	var ticks []time.Time
	s.WithLeaderElection(&recordingTickLease{ticks: &ticks})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	last := time.Date(2027, 3, 14, 12, 0, 0, 0, time.UTC)
	s.catchUp(ctx, last, last.Add(time.Minute), last.Add(time.Hour))
	if len(ticks) != 0 {
		t.Fatalf("cancelled catch-up claimed %d minutes", len(ticks))
	}
}
