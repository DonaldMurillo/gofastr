// Package oz holds the optzero fixtures, reduced from the pre-fix
// round-2 option setters (a6a0d9cff, 6a3a5b663, d32b90af6, e76a69ecb)
// and the WSConfig SendBuffer default (df4727141).
package oz

import (
	"sync/atomic"
	"time"
)

// --- a6a0d9cff: DBQueue WithLeaseTimeout / SetLeaseTimeout ---------

type DBQueue struct {
	lease   time.Duration
	workers int
	table   string
	extra   int // zero default: nothing to replace
}

type DBQueueOption func(*DBQueue)

func NewDBQueue(opts ...DBQueueOption) *DBQueue {
	q := &DBQueue{lease: 5 * time.Minute, workers: 1, table: "queue_jobs"}
	for _, o := range opts {
		o(q)
	}
	return q
}

func WithLeaseTimeout(d time.Duration) DBQueueOption {
	return func(q *DBQueue) { q.lease = d } // want `WithLeaseTimeout stores d into lease with no check`
}

func (q *DBQueue) SetLeaseTimeout(d time.Duration) {
	q.lease = d // want `SetLeaseTimeout stores d into lease with no check`
}

func (q *DBQueue) cutoff(now time.Time) time.Time { return now.Add(-q.lease) }

// WithWorkers: the battery/queue/db.go shape; Start makes a channel of
// the worker total, so -1 panics there.
func WithWorkers(n int) DBQueueOption {
	return func(q *DBQueue) { q.workers = n } // want `WithWorkers stores n into workers with no check`
}

// WithTable: a string parameter is not numeric.
func WithTable(name string) DBQueueOption {
	return func(q *DBQueue) { q.table = name }
}

// WithExtra: the field's default is zero, so storing zero changes
// nothing.
func WithExtra(n int) DBQueueOption {
	return func(q *DBQueue) { q.extra = n }
}

// --- 6a3a5b663: RedisQueue SetVisibilityTimeout (atomic) -------------

type RedisQueue struct {
	visibilityTimeout atomic.Int64
}

func NewRedisQueue() *RedisQueue {
	q := &RedisQueue{}
	q.visibilityTimeout.Store(int64(30 * time.Second))
	return q
}

func (q *RedisQueue) SetVisibilityTimeout(d time.Duration) {
	q.visibilityTimeout.Store(int64(d)) // want `SetVisibilityTimeout stores d into visibilityTimeout with no check`
}

func (q *RedisQueue) cutoff(now time.Time) int64 {
	return now.Add(-time.Duration(q.visibilityTimeout.Load())).UnixNano()
}

// --- d32b90af6: MemoryQueue WithHandlerTimeout -----------------------

const defaultHandlerTimeout = 5 * time.Minute

type MemoryQueue struct{ handlerTimeout time.Duration }

type MemoryQueueOption func(*MemoryQueue)

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{handlerTimeout: defaultHandlerTimeout}
}

func WithHandlerTimeout(d time.Duration) MemoryQueueOption {
	return func(q *MemoryQueue) { q.handlerTimeout = d } // want `WithHandlerTimeout stores d into handlerTimeout with no check`
}

// --- e76a69ecb: outbox WithMaxAttempts / WithPollInterval / WithBatchSize

type Outbox struct {
	maxAttempts  int
	pollInterval time.Duration
	batchSize    int
}

type Option func(*Outbox)

func NewOutbox() *Outbox {
	return &Outbox{maxAttempts: 10, pollInterval: time.Second, batchSize: 100}
}

func WithMaxAttempts(n int) Option {
	return func(o *Outbox) { o.maxAttempts = n } // want `WithMaxAttempts stores n into maxAttempts with no check`
}

func WithPollInterval(d time.Duration) Option {
	return func(o *Outbox) { o.pollInterval = d } // want `WithPollInterval stores d into pollInterval with no check`
}

func WithBatchSize(n int) Option {
	return func(o *Outbox) { o.batchSize = n } // want `WithBatchSize stores n into batchSize with no check`
}
