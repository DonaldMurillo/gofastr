package cron

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStopDeadlineBoundsLoopJoin(t *testing.T) {
	s := NewScheduler()
	s.started = true
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	go func() { done <- s.StopContext(ctx) }()
	select {
	case e := <-done:
		if e == nil {
			t.Error("expected deadline error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("StopContext ignored deadline waiting for loop")
	}
	close(s.stopped)
	<-s.stop
}

type scopeLease struct{}

func (scopeLease) Acquire(context.Context) (bool, func(), error) { return false, nil, nil }

// runTick reads the leader under s.mu, the lock WithLeaderElection
// writes it under. Holding the write lock must hold the tick back; a
// tick that reads the field bare (a data race with a concurrent
// replacement) finishes at once. This fails without -race.
func TestTickReadsLeaderUnderLock(t *testing.T) {
	s := NewScheduler()
	s.WithLeaderElection(scopeLease{})
	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		s.runTick(context.Background(), time.Now())
		close(done)
	}()
	select {
	case <-done:
		s.mu.Unlock()
		t.Fatal("runTick read the leader without taking s.mu")
	case <-time.After(50 * time.Millisecond):
	}
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runTick never finished after the lock was released")
	}
}

func TestLeaderReplacementConcurrent(t *testing.T) {
	s := NewScheduler()
	s.WithLeaderElection(scopeLease{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 500 {
			s.WithLeaderElection(scopeLease{})
		}
	}()
	go func() {
		defer wg.Done()
		for range 500 {
			s.runTick(context.Background(), time.Now())
		}
	}()
	wg.Wait()
}
func TestCronStartingValueStep(t *testing.T) {
	e, err := ParseCron("5/10 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	for _, minute := range []int{5, 15, 25, 35, 45, 55} {
		if !e.matches(time.Date(2026, 1, 1, 0, minute, 0, 0, time.UTC)) {
			t.Fatalf("missing minute %d", minute)
		}
	}
}
