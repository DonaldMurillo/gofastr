// Package claimaudit holds the 2026-11 lifecycle-round shapes: a claim
// handed back on the context the claim ran under (0b23d8584), and audit
// events written on a request's own context after the change they record
// (79cc5197, 9eaba945) or handed to a host sink detached with no bound
// (d799883c).
package claimaudit

import (
	"context"
	"time"
)

type Job struct{ ID, Type string }

type Queue struct{}

func (q *Queue) dequeue(ctx context.Context, lane string) (Job, error) { return Job{}, nil }
func (q *Queue) release(ctx context.Context, job Job) error            { return nil }
func (q *Queue) requeue(ctx context.Context, job Job) error            { return nil }
func (q *Queue) markSeen(ctx context.Context, job Job) error           { return nil }
func (q *Queue) Ack(ctx context.Context, job Job) error                { return nil }

func gateAllows(gate func(string) bool, t string) bool { return gate(t) }

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) { // want settleContext:`detaches\(bounded=true\)`
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

// workerLoop is DBQueue.workerLoop before 0b23d8584: a gated claim handed
// back on the worker's own context. A shutdown between the claim and the
// release fails the UPDATE, and the row stays claimed, its attempt spent.
func (q *Queue) workerLoop(ctx context.Context, gate func(string) bool) {
	for ctx.Err() == nil {
		job, err := q.dequeue(ctx, "")
		if err != nil {
			continue
		}
		if !gateAllows(gate, job.Type) {
			_ = q.release(ctx, job) // want `release hands back the claim taken on line 34 on the context the claim ran under`
			continue
		}
	}
}

// workerLoopFixed is the 0b23d8584 spelling: detached and bounded.
func (q *Queue) workerLoopFixed(ctx context.Context, gate func(string) bool) {
	for ctx.Err() == nil {
		job, err := q.dequeue(ctx, "")
		if err != nil {
			continue
		}
		if !gateAllows(gate, job.Type) {
			settleCtx, cancel := settleContext(ctx)
			_ = q.release(settleCtx, job) // quiet: detached and bounded
			cancel()
		}
	}
}

// Requeue after a claim is a hand-back too.
func (q *Queue) requeueAfterClaim(ctx context.Context) {
	job, err := q.dequeue(ctx, "")
	if err == nil {
		_ = q.requeue(ctx, job) // want `requeue hands back the claim taken on line 62`
	}
}

// A mark after a claim records the claim, not its hand-back: quiet.
func (q *Queue) markAfterClaim(ctx context.Context) {
	job, err := q.dequeue(ctx, "")
	if err == nil {
		_ = q.markSeen(ctx, job)
	}
}

// A release before any claim releases nothing this body took: quiet.
func (q *Queue) releaseFirst(ctx context.Context, job Job) {
	_ = q.release(ctx, job)
	_, _ = q.dequeue(ctx, "")
}

// The release is handed a different root than the claim: quiet.
func (q *Queue) otherRoot(ctx, stopCtx context.Context) {
	job, _ := q.dequeue(ctx, "")
	_ = q.release(stopCtx, job)
}

// A distributed lock released on the context it was taken under: the
// deferred Unlock runs after shutdown cancelled ctx, fails, and the lock
// is held until its TTL.
type Locker interface {
	Lock(ctx context.Context, key string) error
	Unlock(ctx context.Context, key string) error
}

func withLock(ctx context.Context, l Locker, fn func()) error {
	if err := l.Lock(ctx, "k"); err != nil {
		return err
	}
	defer l.Unlock(ctx, "k") // want `Unlock hands back the claim taken on line 97`
	fn()
	return nil
}

// Near miss: a release-named call after a CALLBACK, with no claim in the
// body, is crud's releaseObjects (freeing stored media inside the write),
// not a hand-back: quiet.
type Media struct{}

func (m *Media) releaseObjects(ctx context.Context, id string) error { return nil }

func update(ctx context.Context, m *Media, hook func(context.Context) error) error { // want update:`runsCallback\[0\]`
	if err := hook(ctx); err != nil {
		return err
	}
	return m.releaseObjects(ctx, "id")
}

// requeue after a callback is an outcome like nack.
func retry(ctx context.Context, q *Queue, job Job, h func(context.Context, Job) error) { // want retry:`runsCallback\[0\]`
	if err := h(ctx, job); err != nil {
		_ = q.requeue(ctx, job) // want `requeue records an outcome on the context the callback on line 121 ran under`
	}
}

// litWorkerLoop is workerLoop spelled as a package-level function
// literal: the claim still happens, the hand-back still rides the
// claim's own context.
var litWorkerLoop = func(ctx context.Context, q *Queue) {
	job, err := q.dequeue(ctx, "")
	if err != nil {
		return
	}
	_ = q.release(ctx, job) // want `release hands back the claim taken on line 130 on the context the claim ran under`
}
