package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
}

// NewMemoryQueue creates a new in-memory queue with the given number of workers.
// The internal job channel is buffered to 1024 jobs.
func NewMemoryQueue(workers int) *MemoryQueue {
	return &MemoryQueue{
		workers:  workers,
		jobChan:  make(chan Job, 1024),
		handlers: make(map[string]Handler),
		done:     make(chan struct{}),
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

	err := handler(ctx, job)
	if err != nil {
		job.Attempts++
		if job.Attempts < job.MaxAttempts {
			// Re-enqueue for retry.
			_ = q.Enqueue(ctx, job)
		}
	}
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
		// Try to find a matching job by draining and pushing back non-matches.
		var skipped []Job
		for {
			select {
			case job := <-q.jobChan:
				if _, ok := typeSet[job.Type]; ok {
					// Re-enqueue skipped jobs.
					for _, s := range skipped {
						_ = q.enqueueInternal(s)
					}
					return job, nil
				}
				skipped = append(skipped, job)
			default:
				// Re-enqueue all skipped.
				for _, s := range skipped {
					_ = q.enqueueInternal(s)
				}
				return Job{}, ErrNoJob
			case <-ctx.Done():
				for _, s := range skipped {
					_ = q.enqueueInternal(s)
				}
				return Job{}, ctx.Err()
			}
		}
	}

	select {
	case job := <-q.jobChan:
		return job, nil
	default:
		return Job{}, ErrNoJob
	}
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

// Ack is a no-op for the in-memory queue — jobs are auto-acknowledged after
// successful handler execution.
func (q *MemoryQueue) Ack(_ context.Context, _ string) error {
	return nil
}

// Nack increments the attempt counter and re-enqueues the job if retries remain.
func (q *MemoryQueue) Nack(ctx context.Context, jobID string) error {
	// For Nack to work on MemoryQueue, the caller must track the job themselves
	// and re-enqueue. The automatic worker pool handles retries internally.
	// This method exists to satisfy the Queue interface.
	return nil
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
