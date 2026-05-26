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

// TestCron_EmptyJobName rejects empty names — empty names can collide
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

// TestCron_NilJobFunc refuses to register a job with a nil Run — a nil
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
// crash the process — RunOnce launches each job in a goroutine, and the
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
