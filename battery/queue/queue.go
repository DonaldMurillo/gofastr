// Package queue provides a pluggable job queue with in-memory and Redis backends,
// a goroutine pool for concurrent processing, and scheduled job support.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Job represents a unit of work enqueued for asynchronous processing.
type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	Lane        string          `json:"lane"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	CreatedAt   time.Time       `json:"created_at"`
	ScheduledAt time.Time       `json:"scheduled_at"`
}

// Handler processes a job. Return a non-nil error to trigger a retry.
type Handler func(ctx context.Context, job Job) error

// Queue is the interface that every queue backend must implement.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error
	// Dequeue retrieves and removes the next available job, optionally filtered by type.
	Dequeue(ctx context.Context, types ...string) (Job, error)
	// Ack confirms successful processing of a job.
	Ack(ctx context.Context, jobID string) error
	// Nack marks a job as failed and triggers retry logic.
	Nack(ctx context.Context, jobID string) error
	// Close gracefully shuts down the queue, draining in-progress work.
	Close() error
}

// JobStats is a snapshot of job counts grouped by status. The keys
// are status names ("pending", "running", "failed", "dead").
type JobStats map[string]int

// Browsable is the optional read-only inspection interface — implemented
// by DBQueue so admin tooling can list and aggregate jobs without
// guessing at the underlying schema. Memory and Redis queues may
// implement it later; admin code that depends on it should type-assert.
type Browsable interface {
	// ListJobs returns up to limit jobs in the given status; pass an
	// empty status to return all jobs regardless of state. Jobs are
	// ordered newest-first by created_at.
	ListJobs(ctx context.Context, status string, limit int) ([]Job, error)
	// Stats returns counts grouped by status. Cheap by design — admin
	// dashboards may poll it.
	Stats(ctx context.Context) (JobStats, error)
}

// Replayable is the optional capability for re-queuing a dead-lettered job —
// implemented by DBQueue (the durable backend). Admin tooling type-asserts for
// it and only offers a "replay" action when the backend supports it. Memory and
// Redis queues don't implement it yet (memory drops dead jobs; redis's
// dead-list isn't readable through RedisClient).
type Replayable interface {
	// Replay resets a terminally-failed job back to pending so it is picked up
	// again (attempts counter cleared, scheduled immediately). It MUST only
	// touch terminal ('failed') rows and be idempotent: replaying an unknown,
	// pending, or running job is a no-op, not an error or a double-run.
	Replay(ctx context.Context, jobID string) error
}

// Sentinel errors.
var (
	ErrQueueClosed = errors.New("queue is closed")
	ErrNoJob       = errors.New("no job available")
)
