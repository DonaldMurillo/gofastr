package queue

import (
	"context"
	"testing"
	"time"
)

// The in-memory Scheduler took any interval. Every(5) — five nanoseconds,
// the untyped constant compiles — made tickInterval 5ns, and Start spun a
// timer loop that enqueued one job per iteration, tens of thousands a
// second into a DB-backed queue. DurableScheduler already refuses an
// interval below one millisecond; the in-memory one now does too, and a
// negative interval, which only ever fired on the one-minute floor tick.
// Every(0) keeps registering, as it always has.
func TestSchedulerRefusesSubMillisecondInterval(t *testing.T) {
	for _, d := range []time.Duration{time.Nanosecond, 5, time.Microsecond, time.Millisecond - 1, -time.Second} {
		rq := &recordQueue{}
		s := NewScheduler(rq)
		t.Cleanup(func() { _ = s.Close() })
		if err := s.Every(d).Job("tick", nil).Register(); err == nil {
			t.Errorf("Every(%s) registered", d)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		s.Start(ctx)
		cancel()
		if n := rq.count(); n > 0 {
			t.Errorf("Every(%s): %d jobs enqueued in 20ms", d, n)
		}
	}
	s := NewScheduler(&recordQueue{})
	t.Cleanup(func() { _ = s.Close() })
	for _, d := range []time.Duration{0, time.Millisecond, time.Second} {
		if err := s.Every(d).Job("ok", nil).Register(); err != nil {
			t.Errorf("Every(%s): %v", d, err)
		}
	}
}
