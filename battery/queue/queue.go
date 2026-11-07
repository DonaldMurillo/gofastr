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
	ID           string          `json:"id"`
	OccurrenceID string          `json:"occurrence_id,omitempty"`
	Type         string          `json:"type"`
	Payload      json.RawMessage `json:"payload"`
	Priority     int             `json:"priority"`
	Lane         string          `json:"lane"`
	Attempts     int             `json:"attempts"`
	MaxAttempts  int             `json:"max_attempts"`
	CreatedAt    time.Time       `json:"created_at"`
	ScheduledAt  time.Time       `json:"scheduled_at"`

	// ClaimToken is minted by Dequeue per claim and identifies THAT claim,
	// not only the job. Backends that lease work to possibly-stale workers
	// (RedisQueue) verify it on Ack/Nack: a completion presented for a claim
	// that is no longer current, e.g. a worker whose visibility timeout
	// expired and whose job was re-claimed by another worker, is a fenced
	// no-op instead of mutating the current claimant's lease. Empty for jobs
	// that have never been claimed (and on backends without lease fencing).
	ClaimToken string `json:"claim_token,omitempty"`

	// UserID optionally names the person a job's payload is about. It is
	// what makes the job reachable by App.EraseUserData: queue_jobs lives
	// outside the entity registry, so the erase plane can only find a row
	// through a declared column, and a job carrying an address, an order,
	// or a document in its payload otherwise survives the user's erasure
	// and is re-disclosed by every later App.ExportData dump. Set it
	// whenever the payload is personal data; leave it empty for
	// infrastructure work (cache warms, sweeps, index rebuilds).
	//
	// DBQueue erases the job's row through its queue_jobs eraser;
	// RedisQueue and MemoryQueue remove the job through a store eraser,
	// registered until Close. A schedule's UserID option sets it on every
	// job the schedule fires.
	UserID string `json:"user_id,omitempty"`

	// UniqueKey, when set, makes the job unique among the live jobs of its
	// Type: while one job with this Type and key is pending, claimed, or
	// waiting on a retry, Enqueue refuses another with ErrDuplicateJob.
	// The key is free again once that job succeeds or is dead-lettered.
	// Use it for work where a second copy is wasted or harmful, such as a
	// per-user digest or a reindex of one record. RedisQueue refuses it
	// with ErrUniqueUnsupported.
	UniqueKey string `json:"unique_key,omitempty"`

	// MaxAge bounds how long a failing job keeps retrying, measured from
	// CreatedAt: once a failed attempt's next retry would fall past
	// CreatedAt+MaxAge, the job is dead-lettered instead, whatever
	// attempts it has left. It is checked when an attempt fails, so a job
	// that waited in a backlog still runs once. Zero means no age limit;
	// a negative MaxAge is refused at Enqueue. A Replay keeps CreatedAt,
	// so a replayed job past its age gets the one attempt the replay
	// asked for.
	MaxAge time.Duration `json:"max_age,omitempty"`

	// SequenceKey makes the jobs that share it run one at a time, oldest
	// CreatedAt first, across every job type: a job is not claimed while
	// an older job with its key is pending (including one waiting on a
	// retry) or while any job with its key is running. Use it for changes
	// to one record that must reach a receiver in the order they happened.
	// A dead-lettered job does not hold the key, so the jobs behind it
	// run; replaying it runs it after whichever of them is running.
	// DBQueue only: MemoryQueue and RedisQueue refuse it with
	// ErrSequenceUnsupported.
	SequenceKey string `json:"sequence_key,omitempty"`

	// Status, UpdatedAt and LastError are what a browsing backend
	// (DBQueue's ListJobs) reports about a stored job: its state
	// ("pending", "claimed", "failed", or "done" under
	// WithDoneRetention), when that state last changed, and the error
	// its last failed attempt returned (control bytes scrubbed, at most
	// 500 runes). Enqueue ignores them.
	Status    string    `json:"status,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	LastError string    `json:"last_error,omitempty"`
}

// Handler processes a job. Return a non-nil error to trigger a retry.
// The worker loops of MemoryQueue and DBQueue read two errors in the
// returned chain: one that is [ErrPermanent] dead-letters the job at once,
// and a [*RetryAfterError] (from [RetryAfter]) sets the delay before the
// next attempt.
type Handler func(ctx context.Context, job Job) error

// ErrPermanent marks a handler error that retrying cannot fix, such as a
// 400 or 404 from a receiver: a worker loop that sees it anywhere in the
// returned chain dead-letters the job at once instead of spending its
// remaining attempts. Wrap it with [Permanent] or fmt.Errorf's %w. It
// takes precedence over a [RetryAfter] in the same chain.
var ErrPermanent = errors.New("queue: permanent failure")

// Permanent wraps err so the worker loop dead-letters the job at once.
// The job's LastError keeps err's text.
func Permanent(err error) error {
	if err == nil {
		return ErrPermanent
	}
	return &permanentError{err: err}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string   { return ErrPermanent.Error() + ": " + e.err.Error() }
func (e *permanentError) Unwrap() []error { return []error{ErrPermanent, e.err} }

// RetryAfterError is a handler error that sets the delay before the job's
// next attempt, overriding the queue's backoff or retry schedule, for a
// receiver's Retry-After header or a handler's own schedule. Jitter does
// not apply to it. The job still dead-letters when its attempts run out
// or the next attempt would fall past its MaxAge.
type RetryAfterError struct {
	// Delay is the wait before the next attempt; a negative Delay reads
	// as zero (retry now).
	Delay time.Duration
	// Err is the attempt's failure, kept as the job's LastError.
	Err error
}

func (e *RetryAfterError) Error() string {
	if e.Err == nil {
		return "queue: retry after " + e.Delay.String()
	}
	return e.Err.Error()
}

func (e *RetryAfterError) Unwrap() error { return e.Err }

// RetryAfter returns err as a [*RetryAfterError]: the job's next attempt
// waits d. A handler that reads a Retry-After header bounds the value
// before calling it; MaxAge is the queue's own bound.
func RetryAfter(d time.Duration, err error) error {
	return &RetryAfterError{Delay: max(d, 0), Err: err}
}

// retryDelayFrom reports the delay a handler error asked for, if any.
func retryDelayFrom(err error) (time.Duration, bool) {
	var ra *RetryAfterError
	if errors.As(err, &ra) {
		return max(ra.Delay, 0), true
	}
	return 0, false
}

// pastMaxAge reports whether a retry at next falls past the job's age
// limit.
func pastMaxAge(job Job, next time.Time) bool {
	return job.MaxAge > 0 && next.After(job.CreatedAt.Add(job.MaxAge))
}

// errNegativeMaxAge refuses a job whose MaxAge is negative.
var errNegativeMaxAge = errors.New("queue: Job.MaxAge must not be negative")

// Queue is the interface that every queue backend must implement.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error
	// Dequeue retrieves and removes the next available job, optionally filtered by type.
	Dequeue(ctx context.Context, types ...string) (Job, error)
	// Ack confirms successful processing of the claimed job. Backends with
	// lease fencing (RedisQueue) verify Job.ClaimToken and treat a stale
	// claim's Ack as a no-op.
	Ack(ctx context.Context, job Job) error
	// Nack marks a claimed job as failed and triggers retry logic. Backends
	// with lease fencing (RedisQueue) verify Job.ClaimToken and treat a
	// stale claim's Nack as a no-op.
	Nack(ctx context.Context, job Job) error
	// Close gracefully shuts down the queue, draining in-progress work.
	Close() error
}

// JobStats is a snapshot of job counts grouped by status. The keys
// are status names (DBQueue: "pending", "claimed", "failed", and
// "done" under WithDoneRetention).
type JobStats map[string]int

// Browsable is the optional read-only inspection interface, implemented
// by DBQueue, MemoryQueue and RedisQueue so admin tooling can list and
// aggregate jobs without guessing at the underlying schema.
// MemoryQueue and RedisQueue list dead-letter jobs only; admin code should
// type-assert the capability.
type Browsable interface {
	// ListJobs returns up to limit jobs in the given status, skipping
	// the newest offset of them; pass an empty status to return all jobs
	// regardless of state. Jobs are ordered newest-first by created_at.
	// A negative offset reads as zero.
	ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, error)
	// Stats returns counts grouped by status. Cheap by design, admin
	// dashboards may poll it.
	Stats(ctx context.Context) (JobStats, error)
}

// Replayable is the optional capability for re-queuing a dead-lettered job,
// implemented by DBQueue, MemoryQueue and RedisQueue. Each retains terminally
// failed jobs for inspection and replay. Admin tooling type-asserts for the
// capability before offering a replay action on a supplied Queue.
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

	// ErrDuplicateJob is returned by Enqueue (and Replay) when a live job
	// of the same Type already holds the job's UniqueKey. Nothing was
	// enqueued; callers that only need the work to be queued can treat it
	// as success.
	ErrDuplicateJob = errors.New("queue: a job with this unique key is already queued")

	// ErrUniqueUnsupported is returned by a backend that cannot hold a
	// UniqueKey (RedisQueue). The job is not enqueued.
	ErrUniqueUnsupported = errors.New("queue: this backend does not support unique keys")

	// ErrSequenceUnsupported is returned by a backend that cannot run the
	// jobs of a SequenceKey in order (MemoryQueue, RedisQueue). The job is
	// not enqueued.
	ErrSequenceUnsupported = errors.New("queue: this backend does not support sequence keys")
)
