package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The erasure reached the pending heap, the set Dequeue hands out and the
// dead list, but a job a Start worker was running sat in none of them: the
// worker pops it straight off the heap. When that handler failed, the
// worker put the job back on the heap (or on the dead list), so the erased
// user's payload ran again after an erasure that had reported success.
// The worker's job is now counted and dropped instead of settled.
func TestEraseDropsJobAWorkerIsRunning(t *testing.T) {
	for _, maxAttempts := range []int{3, 1} { // retried, then dead-lettered
		q := NewMemoryQueue(1, WithHandlerTimeout(time.Minute))
		t.Cleanup(func() { _ = q.Close() })
		running := make(chan struct{})
		finish := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(finish) }) }
		t.Cleanup(release) // runs before the Close above on a failed check
		var runs atomic.Int32
		q.RegisterHandler("mail", func(context.Context, Job) error {
			if runs.Add(1) == 1 {
				close(running)
				<-finish
			}
			return errors.New("smtp down")
		})
		q.Start()
		job := userJob("w1", "u1")
		job.MaxAttempts = maxAttempts
		job.UniqueKey = "digest-u1"
		if err := q.Enqueue(context.Background(), job); err != nil {
			t.Fatal(err)
		}
		<-running

		dry, err := q.eraseUser(context.Background(), "u1", true)
		if err != nil || dry != 1 {
			t.Errorf("dry run counted %d jobs (%v), want the running one", dry, err)
		}
		n, err := q.eraseUser(context.Background(), "u1", false)
		if err != nil || n != 1 {
			t.Errorf("erasure counted %d jobs (%v), want the running one", n, err)
		}
		release()
		_ = q.Close() // joins the worker, which has settled by then

		if got := memHolds(q, "u1-secret"); len(got) != 0 {
			t.Errorf("max %d: the erased user's job is back in %v after its handler failed", maxAttempts, got)
		}
		if n := runs.Load(); n != 1 {
			t.Errorf("max %d: the erased job ran %d times, want once", maxAttempts, n)
		}
	}
}
