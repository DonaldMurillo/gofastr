// Package queue provides a pluggable job queue with in-memory, SQL
// (DBQueue) and Redis backends, a goroutine pool for concurrent
// processing, and scheduled job support.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	// ClaimToken identifies the claim Dequeue made; a fenced backend ignores
	// an Ack or Nack of a stale claim.
	ClaimToken string `json:"claim_token,omitempty"`

	// UserID names the person the payload is about, so App.EraseUserData
	// reaches the job.
	UserID string `json:"user_id,omitempty"`

	// UniqueKey, when set, makes Enqueue refuse with ErrDuplicateJob while a
	// live job of the same Type holds the key.
	UniqueKey string `json:"unique_key,omitempty"`

	// MaxAge dead-letters a failing job once its next retry would fall past
	// CreatedAt+MaxAge.
	MaxAge time.Duration `json:"max_age,omitempty"`

	// SequenceKey runs the jobs sharing it one at a time, oldest first, across
	// job types.
	SequenceKey string `json:"sequence_key,omitempty"`

	// Status, UpdatedAt and LastError report a stored job's state to a
	// browsing backend.
	Status    string    `json:"status,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	LastError string    `json:"last_error,omitempty"`
}

// Handler processes a job; an error retries it, unless it wraps ErrPermanent
// or a RetryAfterError.
type Handler func(ctx context.Context, job Job) error

// ErrPermanent marks a handler error retrying cannot fix: the job is
// dead-lettered at once.
var ErrPermanent = errors.New("queue: permanent failure")

// Permanent wraps err so the worker loop dead-letters the job at once.
func Permanent(err error) error {
	if err == nil {
		return ErrPermanent
	}
	return &permanentError{err: err}
}

type permanentError struct{ err error }

func (e *permanentError) Error() string   { return ErrPermanent.Error() + ": " + e.err.Error() }
func (e *permanentError) Unwrap() []error { return []error{ErrPermanent, e.err} }

// RetryAfterError is a handler error that sets the delay before the next
// attempt, overriding backoff.
type RetryAfterError struct {
	// Delay is the wait before the next attempt; a negative Delay reads as
	// zero.
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

// RetryAfter returns err as a RetryAfterError whose next attempt waits d.
func RetryAfter(d time.Duration, err error) error {
	return &RetryAfterError{Delay: max(d, 0), Err: err}
}

func retryDelayFrom(err error) (time.Duration, bool) {
	var ra *RetryAfterError
	if errors.As(err, &ra) {
		return max(ra.Delay, 0), true
	}
	return 0, false
}

func pastMaxAge(job Job, next time.Time) bool {
	return job.MaxAge > 0 && next.After(job.CreatedAt.Add(job.MaxAge))
}

var errNegativeMaxAge = errors.New("queue: Job.MaxAge must not be negative")

// normalizeJob validates age and fills in the ID before setting timestamps
// and defaulting the attempt limit.
func normalizeJob(job *Job, setTimestamps func(*Job)) error {
	if job.MaxAge < 0 {
		return errNegativeMaxAge
	}
	if job.ID == "" {
		job.ID = randomID()
	}
	setTimestamps(job)
	if job.MaxAttempts == 0 {
		job.MaxAttempts = 3
	}
	return nil
}

func defaultCreatedAt(job *Job, now func() time.Time) {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now()
	}
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Queue is the interface that every queue backend must implement.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error
	// Dequeue retrieves and removes the next available job, optionally filtered by type.
	Dequeue(ctx context.Context, types ...string) (Job, error)
	// Ack confirms successful processing of the claimed job.
	Ack(ctx context.Context, job Job) error
	// Nack marks a claimed job failed and triggers its retry.
	Nack(ctx context.Context, job Job) error
	// Close gracefully shuts down the queue, draining in-progress work.
	Close() error
}

// JobStats counts jobs by status, only the jobs ListJobs can list.
type JobStats map[string]int

// Browsable is the optional read-only inspection interface of DBQueue,
// MemoryQueue and RedisQueue; MemoryQueue and RedisQueue list dead-letter jobs only.
type Browsable interface {
	// ListJobs returns up to limit jobs in status ("" for all), newest first,
	// after skipping offset.
	ListJobs(ctx context.Context, status string, limit, offset int) ([]Job, error)
	// Stats returns counts grouped by status.
	Stats(ctx context.Context) (JobStats, error)
}

// Replayable is the optional capability, of DBQueue, MemoryQueue and
// RedisQueue, for re-queuing a dead-lettered job.
type Replayable interface {
	// Replay resets a failed job to pending, its attempts cleared; any other
	// id is a no-op.
	Replay(ctx context.Context, jobID string) error
}

// ReplayReporter is the optional capability, of DBQueue, MemoryQueue and
// RedisQueue, that says whether a replay queued anything.
type ReplayReporter interface {
	// ReplayJob is Replay that reports whether it queued the job again.
	ReplayJob(ctx context.Context, jobID string) (replayed bool, err error)
}

var (
	ErrQueueClosed = errors.New("queue is closed")
	ErrNoJob       = errors.New("no job available")

	// ErrDuplicateJob is returned by Enqueue and Replay when a live job of the
	// same Type holds the UniqueKey.
	ErrDuplicateJob = errors.New("queue: a job with this unique key is already queued")

	// ErrUniqueUnsupported is returned by a backend that cannot hold a
	// UniqueKey (RedisQueue).
	ErrUniqueUnsupported = errors.New("queue: this backend does not support unique keys")

	// ErrSequenceUnsupported is returned by a backend that cannot order a
	// SequenceKey (MemoryQueue, RedisQueue).
	ErrSequenceUnsupported = errors.New("queue: this backend does not support sequence keys")
)
