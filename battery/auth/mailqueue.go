package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// mailQueue delivers mail the auth handlers send off the timed path (the
// password-reset link, the duplicate-registration notice). The handler has
// already written its uniform answer, so the send cannot run on the
// request; a bare goroutine per send would pile up without bound behind a
// stalled provider and outlive shutdown. The queue bounds both: a fixed
// worker pool reads a fixed-capacity channel, a full channel drops the
// send rather than block the handler, every send runs under its own
// deadline, and stop cancels the sends in flight and joins the workers.
//
// The zero value is not ready: build one with newMailQueue. Workers start
// on the first enqueue, so a plugin that never mails starts none.
type mailQueue struct {
	name    string // log label: the plugin the mail belongs to
	timeout time.Duration
	workers int

	mu      sync.Mutex
	queue   chan mailJob
	started bool
	stopped bool
	ctx     context.Context // cancelled by stop
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// mailJob is one queued send. ctx carries the request's values without its
// cancellation (the client already has its answer); send gets a context
// bounded by the queue's per-send timeout and its stop.
type mailJob struct {
	ctx       context.Context
	emailHash string
	send      func(ctx context.Context)
}

const (
	mailQueueCapacity = 1024
	mailQueueWorkers  = 8
	mailSendTimeout   = 30 * time.Second
)

func newMailQueue(name string) *mailQueue {
	return &mailQueue{
		name:    name,
		timeout: mailSendTimeout,
		workers: mailQueueWorkers,
		queue:   make(chan mailJob, mailQueueCapacity),
	}
}

// enqueue schedules send and reports whether it was queued. It never
// blocks: after stop, or with the queue full, the send is dropped and
// logged by the hashed address.
func (q *mailQueue) enqueue(requestCtx context.Context, to string, send func(ctx context.Context)) bool {
	job := mailJob{ctx: context.WithoutCancel(requestCtx), emailHash: hashedIdentifier(to), send: send}
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		slog.Warn("email delivery stopped", "plugin", q.name, "email_hash", job.emailHash)
		return false
	}
	if !q.started {
		q.started = true
		q.ctx, q.cancel = context.WithCancel(context.Background())
		for range q.workers {
			q.wg.Add(1)
			go q.work()
		}
	}
	queued := false
	select {
	case q.queue <- job:
		queued = true
	default:
	}
	q.mu.Unlock()
	if !queued {
		slog.Warn("email delivery queue full", "plugin", q.name, "email_hash", job.emailHash)
	}
	return queued
}

// stop cancels the sends in flight, closes the queue and waits for the
// workers. ctx bounds the wait when a sender ignores cancellation. A
// later enqueue is refused: shutdown is final.
func (q *mailQueue) stop(ctx context.Context) error {
	q.mu.Lock()
	if !q.stopped {
		q.stopped = true
		if q.started {
			q.cancel()
			close(q.queue)
		}
	}
	started := q.started
	q.mu.Unlock()
	if !started {
		return nil
	}
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *mailQueue) work() {
	defer q.wg.Done()
	for job := range q.queue {
		if q.ctx.Err() == nil {
			q.run(job)
		}
	}
}

// run calls one send under the per-send deadline, cancelled early by stop.
// send is host code (a sender, a body template), so a panic is recovered
// here as a backstop; the value is not logged, since a sender's panic can
// carry the body, and a reset body is a takeover link.
func (q *mailQueue) run(job mailJob) {
	defer func() {
		if recover() != nil {
			slog.Error("email delivery panicked", "plugin", q.name, "email_hash", job.emailHash)
		}
	}()
	ctx, cancel := context.WithTimeout(job.ctx, q.timeout)
	stopCancel := context.AfterFunc(q.ctx, cancel)
	defer stopCancel()
	defer cancel()
	job.send(ctx)
}
