package cron_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// TestCron_EmptyJobName rejects empty names, empty names can collide
// across registrations and produce ambiguous log lines.
func TestCron_EmptyJobName(t *testing.T) {
	s := cron.NewScheduler()
	err := s.Register(cron.CronJob{Name: "", Spec: "* * * * *", Run: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatalf("SECURITY: [cron] empty job name was accepted")
	}
	if !errors.Is(err, cron.ErrInvalidJobName) {
		t.Errorf("err = %v; want ErrInvalidJobName", err)
	}
}

// TestCron_VeryLongJobName caps job names at MaxJobNameBytes.
func TestCron_VeryLongJobName(t *testing.T) {
	s := cron.NewScheduler()
	name := strings.Repeat("a", cron.MaxJobNameBytes+1)
	err := s.Register(cron.CronJob{Name: name, Spec: "* * * * *", Run: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatalf("SECURITY: [cron] oversize job name was accepted (%d bytes)", len(name))
	}
}

// TestCron_NilJobFunc refuses to register a job with a nil Run, a nil
// Run would nil-pointer at the next firing.
func TestCron_NilJobFunc(t *testing.T) {
	s := cron.NewScheduler()
	err := s.Register(cron.CronJob{Name: "job", Spec: "* * * * *", Run: nil})
	if err == nil {
		t.Fatalf("SECURITY: [cron] nil Run was accepted")
	}
	if !errors.Is(err, cron.ErrNilJobRun) {
		t.Errorf("err = %v; want ErrNilJobRun", err)
	}
}

// TestScheduler_JobPanicRecovered verifies that a panicking job does not
// crash the process. RunOnce launches each job in a goroutine, and the
// scheduler must defer-recover inside that goroutine before it can route
// the panic to OnError.
func TestScheduler_JobPanicRecovered(t *testing.T) {
	s := cron.NewScheduler()
	var mu sync.Mutex
	var caught error
	s.OnError = func(_ string, err error) {
		mu.Lock()
		defer mu.Unlock()
		caught = err
	}
	if err := s.Register(cron.CronJob{
		Name: "boom",
		Spec: "* * * * *",
		Run:  func(context.Context) error { panic("boom") },
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [cron] RunOnce propagated panic to caller: %v", r)
			}
			close(done)
		}()
		s.RunOnce(context.Background(), time.Now())
	}()
	<-done

	// Give the spawned goroutine time to run and record the panic.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := caught
		mu.Unlock()
		if got != nil {
			if !strings.Contains(got.Error(), "panic") {
				t.Errorf("OnError received non-panic: %v", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("SECURITY: [cron] panic was not surfaced via OnError")
}

// TestScheduler_StopBeforeStart verifies that Stop() returns promptly when
// Start() was never called, a shutdown path that fires the OnStop drainer
// after an aborted boot must not hang forever on a bare channel receive.
func TestScheduler_StopBeforeStart(t *testing.T) {
	cases := []struct {
		name string
		run  func(s *cron.Scheduler)
	}{
		{"never started", func(s *cron.Scheduler) { s.Stop() }},
		{"never started, double stop", func(s *cron.Scheduler) { s.Stop(); s.Stop() }},
		{"started then stopped", func(s *cron.Scheduler) {
			s.Start(context.Background())
			s.Stop()
		}},
		{"stop after stop on started", func(s *cron.Scheduler) {
			s.Start(context.Background())
			s.Stop()
			s.Stop()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := cron.NewScheduler()
			done := make(chan struct{})
			go func() {
				tc.run(s)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("SECURITY: [cron] Stop() deadlocked (%s)", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property: a malformed schedule spec is rejected at PARSE and at
// REGISTRATION time (never deferred to tick time), and the parser never
// panics on adversarial input. Surfaces: cron.Parse (the public parser the
// queue battery uses) and Scheduler.Register (the in-process driver) —
// both must refuse the same spec.
// ---------------------------------------------------------------------------

func TestCron_MalformedSpecRejectedAtParse(t *testing.T) {
	bad := []struct {
		name string
		spec string
	}{
		{"wrong field count", "* * * *"},
		{"minute out of range", "60 * * * *"},
		{"reversed range", "0 0 5-1 * *"},
		{"non-positive step", "*/0 * * * *"},
		{"garbage token", "abc * * * *"},
		{"chained range", "1-2-3 * * * *"},
		{"unknown macro", "@sometimes"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("SECURITY: [cron] Parse(%q) panicked: %v", tc.spec, r)
					}
				}()
				if _, err := cron.Parse(tc.spec); err == nil {
					t.Errorf("SECURITY: [cron] Parse(%q) accepted a malformed spec", tc.spec)
				}
			}()
			s := cron.NewScheduler()
			fired := false
			err := s.Register(cron.CronJob{Name: "j", Spec: tc.spec, Run: func(context.Context) error {
				fired = true
				return nil
			}})
			if err == nil {
				t.Errorf("SECURITY: [cron] Register accepted malformed spec %q; a typo must surface at registration, not at tick time", tc.spec)
			}
			// A rejected job must never fire, even when driven manually.
			s.RunOnce(context.Background(), time.Now())
			if fired {
				t.Errorf("SECURITY: [cron] job with malformed spec %q fired anyway", tc.spec)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property: a spec the parser ACCEPTS only ever matches minutes inside each
// field's documented range, and the accepted-form masks agree with
// Schedule.Matches at the canonical sample each shortcut documents
// (@hourly → top of every hour, @daily → midnight, ...). This pins the
// happy path so the reject table above cannot pass by rejecting everything.
// ---------------------------------------------------------------------------

func TestCron_AcceptedSpecsMatchOnlyInMinute(t *testing.T) {
	specs := []struct {
		spec  string
		match time.Time
		skip  time.Time
	}{
		{"@hourly", time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 7, 1, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		{"@weekly", time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}, // 2026-08-30 is a Sunday
		{"@monthly", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)},
		{"*/15 * * * *", time.Date(2026, 9, 1, 7, 45, 0, 0, time.UTC), time.Date(2026, 9, 1, 7, 50, 0, 0, time.UTC)},
		{"0 9-17 * * 1-5", time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 12, 1, 0, 0, time.UTC)}, // Tue
	}
	for _, tc := range specs {
		t.Run(tc.spec, func(t *testing.T) {
			sc, err := cron.Parse(tc.spec)
			if err != nil {
				t.Fatalf("Parse(%q): %v (documented syntax must parse)", tc.spec, err)
			}
			if !sc.Matches(tc.match) {
				t.Errorf("Matches(%v) = false for %q, want true", tc.match, tc.spec)
			}
			if sc.Matches(tc.skip) {
				t.Errorf("Matches(%v) = true for %q, want false (accepted spec matched outside its range)", tc.skip, tc.spec)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property: Next(t) is the EARLIEST firing minute strictly after t — no
// matching minute is skipped between t and Next(t). Brute-forced against
// Matches for the compound forms (list, range, step, dow range) whose scan
// logic the fixed-date tests in next_test.go do not cover.
// ---------------------------------------------------------------------------

func TestCron_NextIsEarliestMatchNoSkips(t *testing.T) {
	specs := []string{
		"5,35 * * * *",
		"*/20 8-18 * * *",
		"0 0 * * 6,0", // weekends
		"30 4 1 * *",  // monthly
	}
	const maxScan = 366 * 24 * 60 // one year of minutes
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			sc, err := cron.Parse(spec)
			if err != nil {
				t.Fatalf("Parse(%q): %v", spec, err)
			}
			cursor := time.Date(2026, 9, 1, 0, 0, 17, 0, time.UTC) // mid-minute seconds
			for range 3 {
				next := sc.Next(cursor)
				if next.IsZero() {
					t.Fatalf("Next(%v) = zero for satisfiable spec %q", cursor, spec)
				}
				if !next.After(cursor) {
					t.Fatalf("Next(%v) = %v, not strictly after", cursor, next)
				}
				if !sc.Matches(next) {
					t.Fatalf("Next(%v) = %v, but Matches(%v) = false", cursor, next, next)
				}
				// No matching minute may exist between cursor and next.
				for m := cursor.Truncate(time.Minute).Add(time.Minute); m.Before(next); m = m.Add(time.Minute) {
					if sc.Matches(m) {
						t.Fatalf("skipped an earlier firing minute %v: Next(%v) = %v", m, cursor, next)
					}
					if m.Sub(cursor) > maxScan*time.Minute {
						t.Fatalf("scan exceeded a year of minutes")
					}
				}
				cursor = next
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property: an unsatisfiable spec (Feb 30) returns the zero Time from Next
// instead of scanning forever or returning a non-matching minute — the
// documented bounded-horizon contract.
// ---------------------------------------------------------------------------

func TestCron_NextUnsatisfiableReturnsZero(t *testing.T) {
	sc, err := cron.Parse("0 0 30 2 *") // Feb 30 never exists
	if err != nil {
		t.Fatalf("Parse: %v (spec is in-range and must parse)", err)
	}
	if got := sc.Next(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); !got.IsZero() {
		t.Errorf("Next for Feb-30 spec = %v, want zero Time (no minute can ever match)", got)
	}
}
