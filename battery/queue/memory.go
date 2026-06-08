package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MemoryQueue is an in-memory queue backed by a goroutine pool.
type MemoryQueue struct {
	workers  int
	jobChan  chan Job
	handlers map[string]Handler
	wg       sync.WaitGroup
	mu       sync.RWMutex
	closed   bool
	done     chan struct{}

	// holdover stores jobs that were drained by a type-filtered Dequeue but
	// could not be re-enqueued onto the bounded jobChan because it was full at
	// re-enqueue time (concurrent producers refilled it during the drain). It
	// guarantees those valid jobs are never lost; they are re-fed ahead of the
	// channel by subsequent Dequeue/processing. Guarded by holdoverMu.
	holdoverMu sync.Mutex
	holdover   []Job

	// inflight tracks jobs handed out by Dequeue but not yet Ack'd/Nack'd, so
	// Nack(jobID) can re-enqueue the right job. Guarded by inflightMu. The
	// automatic worker pool processes jobs in-line and never touches this map.
	inflightMu sync.Mutex
	inflight   map[string]Job

	// dead retains jobs that exhausted MaxAttempts (terminally failed) so they
	// can be inspected via Browsable and re-queued via Replay, instead of being
	// silently dropped. Ordered oldest-first. It is BOUNDED at maxDeadJobs: when
	// the cap is reached, the oldest dead job is evicted so a flood of failing
	// jobs can never grow memory without limit. Guarded by deadMu.
	deadMu sync.Mutex
	dead   []Job
}

// maxDeadJobs caps the in-memory dead-letter store. Beyond this, the oldest
// retained failed job is dropped to keep memory bounded. The durable DBQueue
// has no such cap (rows persist); this is the price of an in-memory backend.
const maxDeadJobs = 1000

// NewMemoryQueue creates a new in-memory queue with the given number of workers.
// The internal job channel is buffered to 1024 jobs.
func NewMemoryQueue(workers int) *MemoryQueue {
	return &MemoryQueue{
		workers:  workers,
		jobChan:  make(chan Job, 1024),
		handlers: make(map[string]Handler),
		done:     make(chan struct{}),
		inflight: make(map[string]Job),
	}
}

// RegisterHandler registers a handler function for a given job type.
func (q *MemoryQueue) RegisterHandler(jobType string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[jobType] = handler
}

// Start launches the worker goroutines. Must be called before enqueuing jobs
// if you want automatic processing. Workers will call the registered handlers.
func (q *MemoryQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

func (q *MemoryQueue) worker() {
	defer q.wg.Done()
	for job := range q.jobChan {
		q.processJob(job)
	}
}

func (q *MemoryQueue) processJob(job Job) {
	q.mu.RLock()
	handler, ok := q.handlers[job.Type]
	q.mu.RUnlock()

	if !ok {
		// No handler registered — nothing to do.
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := safeHandle(ctx, handler, job)
	if err != nil {
		job.Attempts++
		if job.Attempts < job.MaxAttempts {
			// Re-enqueue for retry.
			_ = q.Enqueue(ctx, job)
		} else {
			// Retries exhausted — retain as terminally-failed for inspection
			// and replay instead of dropping it.
			q.retainDead(job)
		}
	}
}

// safeHandle invokes a handler, converting a panic into an error so a
// poison-message job cannot unwind the worker goroutine and crash the whole
// process. The panicked job follows the normal retry path.
func safeHandle(ctx context.Context, handler Handler, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("queue: handler for %q panicked: %v", job.Type, r)
		}
	}()
	return handler(ctx, job)
}

// Enqueue adds a job to the buffered channel. If the job has no ID, one is generated.
// Uses recover to handle the race between Close() closing the channel and this
// method sending to it.
func (q *MemoryQueue) Enqueue(ctx context.Context, job Job) (err error) {
	// Recover from send on closed channel — Close() can close jobChan
	// between our RLock check and the channel send below.
	defer func() {
		if r := recover(); r != nil {
			err = ErrQueueClosed
		}
	}()

	q.mu.RLock()
	closed := q.closed
	q.mu.RUnlock()
	if closed {
		return ErrQueueClosed
	}

	if job.ID == "" {
		job.ID = randomID()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}

	select {
	case q.jobChan <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dequeue retrieves the next job from the channel. This is useful for manual
// consumption without relying on the automatic worker pool.
func (q *MemoryQueue) Dequeue(ctx context.Context, types ...string) (Job, error) {
	q.mu.RLock()
	closed := q.closed
	q.mu.RUnlock()
	if closed {
		return Job{}, ErrQueueClosed
	}

	// If specific types are requested, drain and re-enqueue non-matching jobs.
	if len(types) > 0 {
		typeSet := make(map[string]struct{}, len(types))
		for _, t := range types {
			typeSet[t] = struct{}{}
		}
		// Drain the holdover first so earlier-skipped jobs are considered before
		// anything still on the channel, then drain the channel itself.
		var skipped []Job
		pending := q.takeHoldover()
		i := 0
		for {
			var job Job
			if i < len(pending) {
				job = pending[i]
				i++
			} else {
				select {
				case job = <-q.jobChan:
				default:
					q.requeueSkipped(skipped)
					return Job{}, ErrNoJob
				case <-ctx.Done():
					q.requeueSkipped(skipped)
					return Job{}, ctx.Err()
				}
			}
			if _, ok := typeSet[job.Type]; ok {
				// Requeue everything we drained but did not consume: the
				// non-matching jobs we skipped plus the not-yet-inspected tail
				// of the holdover we took. None may be dropped.
				if i < len(pending) {
					skipped = append(skipped, pending[i:]...)
				}
				q.requeueSkipped(skipped)
				q.trackInflight(job)
				return job, nil
			}
			skipped = append(skipped, job)
		}
	}

	// Holdover jobs (drained by a prior type-filtered Dequeue but bumped off the
	// full channel) take priority over the channel for untyped consumption.
	if job, ok := q.popHoldover(); ok {
		q.trackInflight(job)
		return job, nil
	}

	select {
	case job := <-q.jobChan:
		q.trackInflight(job)
		return job, nil
	default:
		return Job{}, ErrNoJob
	}
}

// trackInflight records a manually-dequeued job so a later Nack(jobID) can
// re-enqueue it. Jobs with no ID are skipped (Enqueue assigns one, so this is
// only hit for externally-constructed jobs that bypassed Enqueue).
func (q *MemoryQueue) trackInflight(job Job) {
	if job.ID == "" {
		return
	}
	q.inflightMu.Lock()
	q.inflight[job.ID] = job
	q.inflightMu.Unlock()
}

// takeInflight removes and returns a tracked in-flight job by ID.
func (q *MemoryQueue) takeInflight(jobID string) (Job, bool) {
	q.inflightMu.Lock()
	defer q.inflightMu.Unlock()
	job, ok := q.inflight[jobID]
	if ok {
		delete(q.inflight, jobID)
	}
	return job, ok
}

// requeueSkipped returns drained non-matching jobs to the queue without losing
// any: it tries the bounded channel first, and stashes the remainder onto the
// holdover when the channel is full (e.g. concurrent producers refilled it
// during the drain). Holdover jobs are re-fed by subsequent Dequeue calls.
func (q *MemoryQueue) requeueSkipped(skipped []Job) {
	if len(skipped) == 0 {
		return
	}
	var overflow []Job
	for _, s := range skipped {
		if err := q.enqueueInternal(s); err != nil {
			overflow = append(overflow, s)
		}
	}
	if len(overflow) > 0 {
		q.holdoverMu.Lock()
		// Prepend overflow so original ordering is preserved relative to any
		// holdover already present from a concurrent drain.
		q.holdover = append(overflow, q.holdover...)
		q.holdoverMu.Unlock()
	}
}

// takeHoldover atomically removes and returns all currently-held holdover jobs.
func (q *MemoryQueue) takeHoldover() []Job {
	q.holdoverMu.Lock()
	defer q.holdoverMu.Unlock()
	if len(q.holdover) == 0 {
		return nil
	}
	h := q.holdover
	q.holdover = nil
	return h
}

// popHoldover removes and returns the oldest holdover job, if any.
func (q *MemoryQueue) popHoldover() (Job, bool) {
	q.holdoverMu.Lock()
	defer q.holdoverMu.Unlock()
	if len(q.holdover) == 0 {
		return Job{}, false
	}
	job := q.holdover[0]
	q.holdover = q.holdover[1:]
	return job, true
}

// randomID generates a 16-byte hex string ID.
func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// enqueueInternal adds a job without checking the closed flag (for retry re-enqueue).
func (q *MemoryQueue) enqueueInternal(job Job) error {
	select {
	case q.jobChan <- job:
		return nil
	default:
		return ErrQueueClosed
	}
}

// Ack confirms a manually-dequeued job is done, discarding any tracked
// in-flight copy. For jobs processed by the automatic worker pool it is a
// no-op (those are auto-acknowledged after successful handler execution).
func (q *MemoryQueue) Ack(_ context.Context, jobID string) error {
	q.takeInflight(jobID)
	return nil
}

// Nack marks a manually-dequeued job as failed: it increments the attempt
// counter and re-enqueues the job when retries remain, otherwise drops it.
// The job must have been handed out by Dequeue (the in-flight set is consulted
// by ID). Jobs processed by the automatic worker pool retry internally and are
// never in the in-flight set; calling Nack for one is a harmless no-op.
func (q *MemoryQueue) Nack(ctx context.Context, jobID string) error {
	job, ok := q.takeInflight(jobID)
	if !ok {
		// Unknown job (auto-pool retry, or already acked) — nothing to requeue.
		return nil
	}
	job.Attempts++
	if job.MaxAttempts > 0 && job.Attempts >= job.MaxAttempts {
		// Retries exhausted — retain as terminally-failed (inspectable via
		// ListJobs/Stats and re-queuable via Replay) rather than dropping it.
		q.retainDead(job)
		return nil
	}
	return q.Enqueue(ctx, job)
}

// retainDead stores a terminally-failed job in the bounded dead-letter set.
// When the cap is reached, the oldest dead job is evicted so memory stays
// bounded under a flood of failures. The job's Attempts is left at its
// exhausted value so inspection reflects how many tries it took.
func (q *MemoryQueue) retainDead(job Job) {
	if job.ID == "" {
		// An ID is required to inspect/replay a job; skip ID-less jobs.
		return
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	// De-dupe by ID so a re-failed replayed job doesn't appear twice.
	for i, d := range q.dead {
		if d.ID == job.ID {
			q.dead[i] = job
			return
		}
	}
	q.dead = append(q.dead, job)
	if len(q.dead) > maxDeadJobs {
		// Drop the oldest. Copy the retained tail into a fresh slice so the
		// dropped head can be garbage-collected (a reslice would keep it alive).
		drop := len(q.dead) - maxDeadJobs
		kept := make([]Job, maxDeadJobs)
		copy(kept, q.dead[drop:])
		q.dead = kept
	}
}

// ListJobs implements [Browsable] for the in-memory backend. The only state it
// can enumerate is the retained dead-letter set, so it returns those jobs for
// status "failed" (or an empty/"all" status), newest-first, and nothing for any
// other status — pending/claimed jobs live transiently on an unscannable
// channel. limit <= 0 defaults to 100.
func (q *MemoryQueue) ListJobs(_ context.Context, status string, limit int) ([]Job, error) {
	if status != "" && status != "failed" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	q.deadMu.Lock()
	defer q.deadMu.Unlock()
	out := make([]Job, 0, min(limit, len(q.dead)))
	// Newest-first: walk the oldest-first slice in reverse.
	for i := len(q.dead) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, q.dead[i])
	}
	return out, nil
}

// Stats implements [Browsable]. The in-memory backend can only count the
// retained dead-letter jobs (pending/claimed jobs are transient on the
// channel), so it reports those under "failed".
func (q *MemoryQueue) Stats(_ context.Context) (JobStats, error) {
	q.deadMu.Lock()
	n := len(q.dead)
	q.deadMu.Unlock()
	stats := JobStats{}
	if n > 0 {
		stats["failed"] = n
	}
	return stats, nil
}

// Replay implements [Replayable]: it moves a retained terminally-failed job
// back onto the pending set with Attempts reset to 0 so Dequeue returns it
// again. Idempotent and safe: replaying an unknown or non-failed id matches no
// retained job and is a no-op (nil), never a double-enqueue.
func (q *MemoryQueue) Replay(ctx context.Context, jobID string) error {
	q.deadMu.Lock()
	idx := -1
	for i, d := range q.dead {
		if d.ID == jobID {
			idx = i
			break
		}
	}
	if idx == -1 {
		q.deadMu.Unlock()
		return nil // unknown / non-failed id — no-op
	}
	job := q.dead[idx]
	q.dead = append(q.dead[:idx], q.dead[idx+1:]...)
	q.deadMu.Unlock()

	job.Attempts = 0
	return q.Enqueue(ctx, job)
}

// Close drains pending jobs and waits for all workers to finish.
func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	q.mu.Unlock()

	close(q.jobChan)
	q.wg.Wait()
	return nil
}
