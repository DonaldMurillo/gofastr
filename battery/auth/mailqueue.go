package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

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

func scrubbedNoticeSend(plugin, panicMsg, failMsg, to string, sender EmailSender, body string) func(context.Context) {
	return func(ctx context.Context) {
		defer func() {
			if p := recover(); p != nil {
				slog.Warn(panicMsg, "plugin", plugin, "email_hash", hashedIdentifier(to), "panic", textsafe.Recovered(p))
			}
		}()
		if err := sender.Send(ctx, to, body); err != nil {
			slog.Warn(failMsg, "plugin", plugin, "email_hash", hashedIdentifier(to), "err", err)
		}
	}
}

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
