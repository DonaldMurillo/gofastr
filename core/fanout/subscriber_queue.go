package fanout

import (
	"log/slog"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// deliver runs one subscriber callback with panic isolation, mirroring
// [framework/hook].runHookSafely: a subscriber is an extension point, and
// third-party code at an extension point must not be able to take the
// process down.
//
// This goroutine is framework-owned and has no caller frame to unwind
// into, so an unrecovered panic here is fatal. No HTTP middleware
// recover can see it. In a multi-replica deployment every replica
// subscribes to the same topic, so one poison payload would take down
// the whole fleet at once. Dropping the payload and continuing keeps the
// blast radius at one message.
func deliver(fn func([]byte), p []byte) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: subscriber panicked — payload dropped",
				slog.Any("panic", textsafe.Recovered(r)))
		}
	}()
	fn(p)
}

// SubscriberQueue wraps a subscriber callback in a per-subscriber bounded
// queue with drop-oldest overflow, running fn on a dedicated goroutine.
//
// It is an implementation aid for [Fanout] backends whose transport delivers
// payloads on a SHARED goroutine, e.g. a single LISTEN/NOTIFY dispatcher
// (framework/fanout) or a Redis reader goroutine (NewRedis). Wrapping each
// subscriber in its own queue preserves the per-subscriber bounded-queue +
// drop-oldest contract the [Fanout.Subscribe] doc promises, so one slow
// subscriber cannot stall delivery to the others (and never blocks the shared
// transport goroutine). [InProcess] uses it internally too.
//
// send is safe to call concurrently and NEVER blocks: when the queue is full
// it drops the oldest queued payload and enqueues the new one. After stop,
// send is a silent no-op. stop signals the goroutine to exit and returns
// promptly; safe to call multiple times.
//
// depth <= 0 selects the default queue depth.
func SubscriberQueue(fn func([]byte), depth int) (send func([]byte), stop func()) {
	sendItem, stop := dropOldestQueue(func(p []byte) { deliver(fn, p) }, depth, nil)
	send = func(payload []byte) {
		sendItem(append([]byte(nil), payload...))
	}
	return send, stop
}

// dropOldestQueue is the machinery behind SubscriberQueue and
// PublishQueueReporting: a bounded queue drained by one dedicated
// goroutine that runs fn per item. send never blocks: when the queue is
// full it drops the oldest queued item, handing it to dropped (when
// non-nil) on the sender's goroutine, and enqueues the new one. After
// stop, send is a silent no-op.
func dropOldestQueue[T any](fn func(T), depth int, dropped func(T)) (send func(T), stop func()) {
	if depth <= 0 {
		depth = defaultInProcessQueue
	}
	q := make(chan T, depth)
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-stopped:
				return
			case item := <-q:
				fn(item)
			}
		}
	}()
	send = func(item T) {
		for {
			select {
			case <-stopped:
				return
			case q <- item:
				return
			default:
			}
			// Queue full: drop oldest and retry. Receiving from a buffered
			// channel needs no concurrent consumer, so this pops a queued
			// item even while fn is still running, guaranteeing forward
			// progress (no busy-spin) and the drop-oldest contract. stopped
			// is re-checked every iteration.
			select {
			case old := <-q:
				if dropped != nil {
					dropped(old)
				}
			case <-stopped:
				return
			default:
				// drained between selects; retry the send
			}
		}
	}
	// stop signals the goroutine to exit at its next select and returns
	// promptly. It does not wait for an in-flight fn (fn must not block per
	// the Fanout contract; a blocking fn would make any wait deadlock), nor
	// does it drain queued items. This is a lossy lane, so dropping the
	// few still-queued messages on unsubscribe is the intended behavior.
	stop = func() {
		once.Do(func() { close(stopped) })
	}
	return send, stop
}
