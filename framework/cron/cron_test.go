package cron

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// parseCron — accepts standard 5-field syntax + the @shortcuts.
// ============================================================================

func TestCron_Parse_StandardAndShortcuts(t *testing.T) {
	cases := []string{
		"* * * * *",
		"0 * * * *",
		"30 4 * * *",
		"*/5 * * * *",
		"0 0 1 1 *",
		"15,45 * * * *",
		"0 9-17 * * 1-5",
		"@hourly",
		"@daily",
		"@weekly",
		"@monthly",
		"@yearly",
	}
	for _, spec := range cases {
		if _, err := parseCron(spec); err != nil {
			t.Errorf("parseCron(%q): %v", spec, err)
		}
	}
}

// ============================================================================
// parseCron rejects malformed input cleanly.
// ============================================================================

func TestCron_Parse_RejectsBadInput(t *testing.T) {
	bad := []string{
		"",          // empty
		"* * * *",   // too few fields
		"* * * * * *", // too many
		"60 * * * *", // minute > 59
		"* 24 * * *", // hour > 23
		"* * 0 * *",  // day-of-month < 1
		"* * * 13 *", // month > 12
		"* * * * 7",  // dow > 6
		"a * * * *",  // not a number
		"*/0 * * * *", // step 0
	}
	for _, spec := range bad {
		if _, err := parseCron(spec); err == nil {
			t.Errorf("parseCron(%q): expected error, got nil", spec)
		}
	}
}

// ============================================================================
// matches — verifies the bitmask check against a concrete clock.
// ============================================================================

func TestCron_Matches(t *testing.T) {
	// Tuesday 2025-04-08 09:30:00
	tue := time.Date(2025, 4, 8, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		spec  string
		match bool
	}{
		{"* * * * *", true},
		{"30 9 * * *", true},
		{"30 9 8 4 2", true},   // Tuesday=2
		{"0 9 * * *", false},   // minute 0 != 30
		{"30 9 * * 1", false},  // Monday only
		{"*/15 * * * *", true}, // 0,15,30,45
		{"*/15 * * * *", true},
	}
	for _, c := range cases {
		e, err := parseCron(c.spec)
		if err != nil {
			t.Fatalf("parse %q: %v", c.spec, err)
		}
		if got := e.matches(tue); got != c.match {
			t.Errorf("%q.matches(Tue 09:30): got %v want %v", c.spec, got, c.match)
		}
	}
}

// ============================================================================
// runOnce dispatches matching jobs and skips non-matching ones.
// ============================================================================

func TestCron_RunOnceDispatchesMatchingJobs(t *testing.T) {
	s := NewScheduler()
	var fired int32
	if err := s.Register(CronJob{
		Name: "every-minute",
		Spec: "* * * * *",
		Run: func(_ context.Context) error {
			atomic.AddInt32(&fired, 1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(CronJob{
		Name: "midnight-only",
		Spec: "0 0 * * *",
		Run: func(_ context.Context) error {
			atomic.AddInt32(&fired, 1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	noon := time.Date(2025, 4, 8, 12, 0, 0, 0, time.UTC)
	s.RunOnce(context.Background(), noon)

	// Goroutines launched by runOnce — give them a moment.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&fired) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Both jobs match minute=0; "every-minute" matches always, "midnight-only"
	// only when hour=0. At noon only one should fire.
	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Fatalf("expected 1 firing at noon, got %d", got)
	}
}

// ============================================================================
// OnError forwards job errors.
// ============================================================================

func TestCron_OnErrorForwarded(t *testing.T) {
	s := NewScheduler()
	captured := make(chan string, 1)
	s.OnError = func(name string, err error) {
		captured <- fmt.Sprintf("%s: %v", name, err)
	}
	boom := errors.New("kaboom")
	if err := s.Register(CronJob{
		Name: "failing",
		Spec: "* * * * *",
		Run:  func(_ context.Context) error { return boom },
	}); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), time.Now())

	select {
	case msg := <-captured:
		if msg != "failing: kaboom" {
			t.Fatalf("OnError got %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("OnError not invoked within 1s")
	}
}

// ============================================================================
// Register rejects bad specs at registration time, not at firing time.
// ============================================================================

func TestCron_RegisterRejectsBadSpec(t *testing.T) {
	s := NewScheduler()
	err := s.Register(CronJob{
		Name: "garbage",
		Spec: "not a cron",
		Run:  func(_ context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error on bad spec, got nil")
	}
}

// ============================================================================
// Stop is idempotent and unblocks Start's loop.
// ============================================================================

func TestCron_StopIdempotent(t *testing.T) {
	s := NewScheduler()
	s.Start(context.Background())
	s.Stop()
	s.Stop() // must not panic or hang
}
