package signal

import "sync"

// Effect runs a function immediately and re-runs it whenever any signal
// read inside the function changes. Returns a dispose function to stop the effect.
func Effect(fn func()) func() {
	e := &effect{
		fn: fn,
	}

	// First run with tracking
	stop := startTracking()
	fn()
	deps := stop()

	// Subscribe to all dependencies
	for dep := range deps {
		if sub, ok := dep.(Subscribable); ok {
			unsub := sub.subscribeInternal(e.run)
			e.unsubs = append(e.unsubs, unsub)
		}
	}

	return e.dispose
}

type effect struct {
	fn     func()
	unsubs []func()
	mu     sync.Mutex
}

func (e *effect) run() {
	e.fn()
}

func (e *effect) dispose() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, unsub := range e.unsubs {
		unsub()
	}
	e.unsubs = nil
}

// Batch state
var (
	batchMu      sync.Mutex
	batching     bool
	batchPending []func()
)

// Batch defers subscriber notifications until the function completes.
// All signal changes within fn are collected, and subscribers are
// notified once at the end.
func Batch(fn func()) {
	batchMu.Lock()
	if batching {
		batchMu.Unlock()
		fn() // already batching, just run
		return
	}
	batching = true
	batchPending = nil
	batchMu.Unlock()

	fn()

	batchMu.Lock()
	batching = false
	pending := batchPending
	batchPending = nil
	batchMu.Unlock()

	for _, p := range pending {
		p()
	}
}

// enqueueBatch tries to enqueue a notification in batch mode.
// Returns true if the notification was enqueued (batch active),
// false if the caller should proceed with immediate notification.
func enqueueBatch(notify func()) bool {
	batchMu.Lock()
	if !batching {
		batchMu.Unlock()
		return false
	}
	batchPending = append(batchPending, notify)
	batchMu.Unlock()
	return true
}
