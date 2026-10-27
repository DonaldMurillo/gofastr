package cron

import (
	"context"
	"sync"
	"testing"
	"time"
)

// slowLease takes 40ms to answer and records when each tick asked.
type slowLease struct {
	mu   sync.Mutex
	asks []time.Time
}

func (l *slowLease) Acquire(context.Context) (bool, func(), error) {
	l.mu.Lock()
	l.asks = append(l.asks, time.Now())
	l.mu.Unlock()
	time.Sleep(40 * time.Millisecond)
	return false, nil, nil
}

// Ticks stay on the interval's boundaries however long a tick takes:
// rearming one interval after the work let the delay pile up until a
// whole minute was skipped.
func TestTickTimeDoesNotDrift(t *testing.T) {
	lease := &slowLease{}
	s := NewScheduler()
	s.tickEv = 100 * time.Millisecond
	s.WithLeaderElection(lease)
	s.Start(context.Background())
	time.Sleep(650 * time.Millisecond)
	s.Stop()

	lease.mu.Lock()
	defer lease.mu.Unlock()
	if len(lease.asks) < 4 {
		t.Fatalf("%d ticks in 650ms, want at least 4", len(lease.asks))
	}
	for i, at := range lease.asks {
		if off := at.Sub(at.Truncate(s.tickEv)); off > 30*time.Millisecond {
			t.Errorf("tick %d fired %v past its boundary", i, off)
		}
	}
}
