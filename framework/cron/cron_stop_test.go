package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Graceful shutdown joins in-flight job goroutines: Stop returning while
// a job is mid-write means the process exits under it on SIGTERM.
func TestStopWaitsForInflightJobs(t *testing.T) {
	s := NewScheduler()
	var finished atomic.Bool
	err := s.Register(CronJob{Name: "slow", Spec: "* * * * *", Run: func(ctx context.Context) error {
		time.Sleep(150 * time.Millisecond)
		finished.Store(true)
		return nil
	}})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	s.RunOnce(context.Background(), time.Now())
	s.Stop()
	if !finished.Load() {
		t.Fatal("Stop returned while a job was still in flight")
	}
}

// A job that ignores its context must not hang shutdown forever:
// StopContext abandons the join at the deadline and reports it.
func TestStopContextAbandonsHungJob(t *testing.T) {
	s := NewScheduler()
	hang := make(chan struct{})
	defer close(hang)
	err := s.Register(CronJob{Name: "hung", Spec: "* * * * *", Run: func(ctx context.Context) error {
		<-hang
		return nil
	}})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	s.RunOnce(context.Background(), time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s.StopContext(ctx); err == nil {
		t.Fatal("StopContext should report the abandoned in-flight job")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("StopContext did not honor its deadline")
	}
}

// Stop and StopContext may race (App's SIGTERM drainer vs. a user OnStop
// hook); a double close of the stop channel would turn graceful shutdown
// into a panic exit.
func TestConcurrentStopsDontPanic(t *testing.T) {
	s := NewScheduler()
	s.Start(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}
