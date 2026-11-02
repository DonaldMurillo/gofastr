// Package a reduces battery/queue's DBQueue.workerLoop before and after
// c9f60506f, with the postures the rule leaves alone.
package a

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

var ErrNoJob = errors.New("no job")

type job struct{ ID string }

type queue struct {
	stop    chan struct{}
	logger  *slog.Logger
	failing bool
}

func (q *queue) dequeue(ctx context.Context) (*job, error) { return nil, ErrNoJob }
func (q *queue) run(j *job)                                {}

// workerLoop is the pre-fix claim loop: every error waits and retries.
func (q *queue) workerLoop(ctx context.Context) {
	wait := func() bool {
		t := time.NewTimer(100 * time.Millisecond)
		defer t.Stop()
		select {
		case <-q.stop:
			return true
		case <-ctx.Done():
			return true
		case <-t.C:
			return false
		}
	}
	for {
		j, err := q.dequeue(ctx)
		if err != nil { // want `err is waited out and retried with no trace`
			// ErrNoJob is the steady state, sleep briefly and retry.
			if wait() {
				return
			}
			continue
		}
		q.run(j)
	}
}

// sleepRetry waits inline.
func (q *queue) sleepRetry(ctx context.Context, ids []string) {
	for range ids {
		if _, err := q.dequeue(ctx); err != nil { // want `err is waited out and retried`
			time.Sleep(time.Second)
			continue
		}
	}
}

func (q *queue) backoff() { <-time.After(time.Second) }

// helperWait waits through a same-package method.
func (q *queue) helperWait(ctx context.Context) {
	for {
		if _, err := q.dequeue(ctx); err != nil { // want `err is waited out and retried`
			q.backoff()
			continue
		}
		return
	}
}

// ---- quiet ----

func (q *queue) noteClaimErr(err error) {
	if !q.failing {
		q.failing = true
		q.logger.Error("claim failed", "err", err)
	}
}

// fixed is the c9f60506f loop: the outage is noted, the steady state is
// tested with errors.Is.
func (q *queue) fixed(ctx context.Context) {
	for {
		j, err := q.dequeue(ctx)
		if err != nil {
			if !errors.Is(err, ErrNoJob) && ctx.Err() == nil {
				q.noteClaimErr(err)
			}
			time.Sleep(time.Second)
			continue
		}
		q.run(j)
	}
}

// sentinel: the condition itself looked at the error.
func (q *queue) sentinel(ctx context.Context) {
	for {
		_, err := q.dequeue(ctx)
		if errors.Is(err, ErrNoJob) {
			time.Sleep(time.Second)
			continue
		}
		if err != nil && ctx.Err() != nil {
			time.Sleep(time.Second)
			continue
		}
		return
	}
}

// logged without err, or counted: the skip leaves a trace.
func (q *queue) traced(ctx context.Context, misses *int) {
	for {
		if _, err := q.dequeue(ctx); err != nil {
			q.logger.Warn("claim failed; retrying")
			time.Sleep(time.Second)
			continue
		}
		if _, err := q.dequeue(ctx); err != nil {
			*misses++
			time.Sleep(time.Second)
			continue
		}
		return
	}
}

// remembered keeps the error for a later report: a read.
func (q *queue) remembered(ctx context.Context) error {
	var last error
	for i := 0; i < 3; i++ {
		if _, err := q.dequeue(ctx); err != nil {
			last = err
			time.Sleep(time.Second)
			continue
		}
		return nil
	}
	return last
}

// noWait skips without backing off: a filter, out of scope.
func (q *queue) noWait(ctx context.Context, ids []string) {
	for range ids {
		if _, err := q.dequeue(ctx); err != nil {
			continue
		}
	}
}

// literal: the wait sits in a nested func literal the block only builds.
func (q *queue) literal(ctx context.Context) {
	for {
		if _, err := q.dequeue(ctx); err != nil {
			go func() { time.Sleep(time.Second) }()
			continue
		}
		return
	}
}

// waitThenReturn waits but does not retry.
func (q *queue) waitThenReturn(ctx context.Context) {
	for {
		if _, err := q.dequeue(ctx); err != nil {
			time.Sleep(time.Second)
			return
		}
	}
}

// notError: a bool named like an error.
func (q *queue) notError(ok func() bool) {
	for {
		var err *int
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		return
	}
}
