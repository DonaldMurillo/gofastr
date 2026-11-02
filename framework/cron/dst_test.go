package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// ============================================================================
// The in-process Scheduler matched each tick's wall clock with no DST
// handling. On a fall-back night a fixed-time job ("30 1 * * *") matched
// both the daylight and the standard 01:30 and ran twice; on a
// spring-forward day a job whose time sat in the skipped hour
// ("30 2 * * *") matched no tick and did not run at all. Leader election
// did not help: the tick claim is keyed per UTC minute, and the two 01:30s
// are different UTC minutes. The Scheduler now follows vixie cron: a
// fixed-time job runs once in a repeated hour and, when its time is
// skipped, once right after the jump; a job with a wildcard minute or
// hour runs by real elapsed time.
// ============================================================================

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata for %s unavailable: %v", name, err)
	}
	return loc
}

// fireTimes drives a scheduler holding one job, minute by minute in real
// time from start (inclusive) to end (exclusive), with the tick clock in
// loc, and returns the ticks at which the job ran. le, when set, is the
// leader election every scheduler in schedulers shares; each tick is
// offered to every scheduler in turn, as replicas whose timers fire for
// the same minute.
func fireTimes(t *testing.T, spec string, loc *time.Location, start, end time.Time, le LeaderElection, replicas int) []time.Time {
	t.Helper()
	var runs atomic.Int32
	var schedulers []*Scheduler
	for range max(replicas, 1) {
		s := NewScheduler()
		if le != nil {
			s.WithLeaderElection(le)
		}
		if err := s.Register(CronJob{Name: "j", Spec: spec, Run: func(context.Context) error {
			runs.Add(1)
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
		schedulers = append(schedulers, s)
	}
	ctx := context.Background()
	var fired []time.Time
	for tick := start; tick.Before(end); tick = tick.Add(time.Minute) {
		for _, s := range schedulers {
			before := runs.Load()
			s.runTick(ctx, tick.In(loc).Add(5*time.Millisecond)) // a timer fires just past the minute
			s.inflight.Wait()
			if runs.Load() != before {
				fired = append(fired, tick.In(loc))
			}
		}
	}
	return fired
}

func wantFires(t *testing.T, label string, got []time.Time, want ...time.Time) {
	t.Helper()
	ok := len(got) == len(want)
	for i := 0; ok && i < len(want); i++ {
		ok = got[i].Equal(want[i])
	}
	if !ok {
		t.Errorf("%s: fired at %v, want %v", label, got, want)
	}
}

func TestFixedTimeJobAcrossDST(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	lh := mustZone(t, "Australia/Lord_Howe")
	edt := time.FixedZone("EDT", -4*3600)
	est := time.FixedZone("EST", -5*3600)
	lhdt := time.FixedZone("+11", 11*3600)
	lhst := time.FixedZone("+1030", 10*3600+1800)
	cases := []struct {
		name, spec string
		loc        *time.Location
		start, end time.Time
		want       []time.Time
	}{
		{"NY fall-back, time in the repeated hour", "30 1 * * *", ny,
			time.Date(2026, 11, 1, 0, 0, 0, 0, edt), time.Date(2026, 11, 1, 4, 0, 0, 0, est),
			[]time.Time{time.Date(2026, 11, 1, 1, 30, 0, 0, edt)}},
		{"NY spring-forward, time in the gap", "30 2 * * *", ny,
			time.Date(2026, 3, 8, 0, 0, 0, 0, est), time.Date(2026, 3, 8, 5, 0, 0, 0, edt),
			[]time.Time{time.Date(2026, 3, 8, 3, 0, 0, 0, edt)}},
		{"NY spring-forward, time just after the gap", "0 3 * * *", ny,
			time.Date(2026, 3, 8, 0, 0, 0, 0, est), time.Date(2026, 3, 8, 5, 0, 0, 0, edt),
			[]time.Time{time.Date(2026, 3, 8, 3, 0, 0, 0, edt)}},
		{"Lord Howe fall-back (30 minutes), time in the repeat", "45 1 * * *", lh,
			time.Date(2026, 4, 5, 0, 0, 0, 0, lhdt), time.Date(2026, 4, 5, 4, 0, 0, 0, lhst),
			[]time.Time{time.Date(2026, 4, 5, 1, 45, 0, 0, lhdt)}},
		{"Lord Howe spring-forward (30 minutes), time in the gap", "15 2 * * *", lh,
			time.Date(2026, 10, 4, 0, 0, 0, 0, lhst), time.Date(2026, 10, 4, 4, 0, 0, 0, lhdt),
			[]time.Time{time.Date(2026, 10, 4, 2, 30, 0, 0, lhdt)}},
		{"UTC", "30 1 * * *", time.UTC,
			time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s, _ := c.start.In(c.loc).ZoneBounds(); c.loc != time.UTC && !s.Before(c.end) {
				t.Fatalf("no zone transition inside the window; tzdata changed?")
			}
			wantFires(t, c.spec, fireTimes(t, c.spec, c.loc, c.start, c.end, nil, 1), c.want...)
		})
	}
}

// A job whose minute or hour is a wildcard keeps real elapsed time across
// both transitions: as many runs as the window holds real intervals, no
// duplicate and no gap.
func TestWildcardJobKeepsRealTimeAcrossDST(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	lh := mustZone(t, "Australia/Lord_Howe")
	windows := []struct {
		name       string
		loc        *time.Location
		start, end time.Time
	}{
		{"NY fall-back", ny, time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)},
		{"NY spring-forward", ny, time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)},
		{"Lord Howe fall-back", lh, time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC), time.Date(2026, 4, 4, 17, 0, 0, 0, time.UTC)},
		{"Lord Howe spring-forward", lh, time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)},
		{"UTC", time.UTC, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)},
	}
	for _, w := range windows {
		t.Run(w.name, func(t *testing.T) {
			hours := int(w.end.Sub(w.start) / time.Hour)
			got := fireTimes(t, "*/15 * * * *", w.loc, w.start, w.end, nil, 1)
			if len(got) != 4*hours {
				t.Errorf("*/15 fired %d times in %d real hours, want %d: %v", len(got), hours, 4*hours, got)
			}
			for i := 1; i < len(got); i++ {
				if d := got[i].Sub(got[i-1]); d != 15*time.Minute {
					t.Errorf("*/15: %s between %s and %s, want 15m", d, got[i-1], got[i])
				}
			}
			if w.loc == lh {
				return // "0 * * * *" on a 30-minute shift lands 30 or 90 minutes apart by the wall clock
			}
			if got := fireTimes(t, "0 * * * *", w.loc, w.start, w.end, nil, 1); len(got) != hours {
				t.Errorf("0 * * * * fired %d times in %d real hours: %v", len(got), hours, got)
			}
		})
	}
}

// Two replicas sharing a tick-claiming lease: the claim is per UTC minute,
// so it never stopped the second 01:30 of a fall-back night. Now neither
// replica fires the repeat, so the job still runs once.
func TestLeaderElectedFallBackRunsOnce(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	want := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC) // 01:30 EDT
	t.Run("fake", func(t *testing.T) {
		got := fireTimes(t, "30 1 * * *", ny, start, end, fakeTickLease{&tickBook{}}, 2)
		wantFires(t, "fake lease", got, want)
	})
	t.Run("postgres", func(t *testing.T) {
		lease := NewPostgresAdvisoryLease(pgtest.DB(t), 4242)
		got := fireTimes(t, "30 1 * * *", ny, start, end, lease, 2)
		wantFires(t, "postgres lease", got, want)
	})
}
