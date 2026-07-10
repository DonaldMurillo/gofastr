package cron

import (
	"context"
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
