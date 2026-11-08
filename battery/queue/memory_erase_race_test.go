package queue

import (
	"testing"
	"time"
)

// The erasure/settle window: settleRunning reads rj.erased, then re-adds
// the job to a store. If an eraseUser ran between those two steps, it
// would count the job as gone, clear every store, and return — and the
// settle would carry the user's job back into pending where no erasure
// will ever look again (#659 loose ends). moveMu closes the window: the
// settle holds the read side across both steps, so it cannot make
// progress while an erasure holds the write side, and the erasure
// either sets rj.erased first (the settle is skipped) or finds the
// re-added job in pending.
func TestSettleCannotRunUnderAnErasure(t *testing.T) {
	q := NewMemoryQueue(0)
	job := Job{ID: "job-1", Type: "work", UserID: "u1"}
	rj := &runningJob{job: job}
	q.inflightMu.Lock()
	q.inflight[job.ID] = job
	q.running = append(q.running, rj)
	q.inflightMu.Unlock()

	// Stand in for an eraseUser in mid-erase: it holds moveMu across the
	// whole read-and-clear of the three stores.
	q.moveMu.Lock()

	started := make(chan struct{})
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		q.settleRunning(rj, func() {
			close(entered)
			<-release
			q.pmu.Lock()
			q.pushLocked(job)
			q.pmu.Unlock()
		})
	}()
	<-started

	// The settle goroutine is inside settleRunning with only moveMu
	// between it and the settle. With the read side held by the test,
	// the settle must not start.
	select {
	case <-entered:
		t.Fatal("a running job's settle ran while an erasure holds moveMu: the erasure can no longer see the re-added job")
	case <-time.After(300 * time.Millisecond):
	}

	// Release the erasure; the settle completes and re-adds the job,
	// proving the goroutine was parked on the lock, not stalled.
	close(release)
	q.moveMu.Unlock()
	<-done

	q.pmu.Lock()
	defer q.pmu.Unlock()
	found := false
	for _, pj := range q.pending.items {
		if pj.job.ID == job.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the settle never re-added the job after the erasure released moveMu")
	}
}
