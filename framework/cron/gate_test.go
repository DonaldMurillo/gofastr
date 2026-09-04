package cron

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSetGateSkipsDisabledJob(t *testing.T) {
	s := NewScheduler()
	ran := false
	if err := s.Register(CronJob{
		Name: "gated",
		Spec: "* * * * *",
		Run:  func(ctx context.Context) error { ran = true; return nil },
	}); err != nil {
		t.Fatal(err)
	}

	s.SetGate(func(jobName string) bool {
		return jobName != "gated"
	})

	s.RunOnce(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	if ran {
		t.Fatal("gated job ran despite gate returning false")
	}
}

func TestSetGateAllowsEnabledJob(t *testing.T) {
	s := NewScheduler()
	ran := false
	if err := s.Register(CronJob{
		Name: "open",
		Spec: "* * * * *",
		Run:  func(ctx context.Context) error { ran = true; return nil },
	}); err != nil {
		t.Fatal(err)
	}

	s.SetGate(func(jobName string) bool { return true })

	s.RunOnce(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	s.Stop()

	if !ran {
		t.Fatal("open job did not run despite gate returning true")
	}
}

func TestSetGateNilIsNoop(t *testing.T) {
	s := NewScheduler()
	s.SetGate(nil)
	ran := false
	if err := s.Register(CronJob{
		Name: "test",
		Spec: "* * * * *",
		Run:  func(ctx context.Context) error { ran = true; return nil },
	}); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	s.Stop()
	if !ran {
		t.Fatal("job did not run when gate is nil")
	}
}

// TestGatePanicLoggedWithoutOnError: a recovered gate panic must reach
// an observable sink even when no OnError callback is configured. This
// test carries the gate-panic containment contract (with OnError set,
// the panic routes there instead — see leader_security_test.go and
// TestScheduler_JobPanicRecovered).
// reportError used to return early on a nil OnError, so the panic
// value and stack were dropped on the floor while the job silently
// skipped.
func TestGatePanicLoggedWithoutOnError(t *testing.T) {
	s := NewScheduler()
	s.SetGate(func(string) bool { panic("gate boom") })
	ran := false
	if err := s.Register(CronJob{
		Name: "j",
		Spec: "* * * * *",
		Run:  func(ctx context.Context) error { ran = true; return nil },
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s.RunOnce(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	s.Stop()

	if ran {
		t.Fatal("a panicking gate must deny the job for this tick")
	}
	out := buf.String()
	if !strings.Contains(out, "gate boom") {
		t.Fatalf("recovered gate panic dropped with OnError == nil; log = %q", out)
	}
}

// TestJobErrorLoggedWithoutOnError pins the same contract for plain
// job errors: with no OnError configured they go to slog.Default()
// instead of vanishing.
func TestJobErrorLoggedWithoutOnError(t *testing.T) {
	s := NewScheduler()
	if err := s.Register(CronJob{
		Name: "failing",
		Spec: "* * * * *",
		Run:  func(ctx context.Context) error { return errors.New("kaboom") },
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s.RunOnce(context.Background(), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	s.Stop()

	// The job goroutine logs asynchronously; Stop() joins it, so the
	// log line has landed by the time Stop returns.
	if !strings.Contains(buf.String(), "kaboom") {
		t.Fatalf("job error dropped with OnError == nil; log = %q", buf.String())
	}
}
