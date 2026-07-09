package fanout

import "sync"

// SubscriberQueue wraps a subscriber callback in a per-subscriber bounded
// queue with drop-oldest overflow, running fn on a dedicated goroutine.
//
// It is an implementation aid for [Fanout] backends whose transport delivers
// payloads on a SHARED goroutine — e.g. a single LISTEN/NOTIFY dispatcher
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
	if depth <= 0 {
		depth = defaultInProcessQueue
	}
	q := make(chan []byte, depth)
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case <-stopped:
				return
			case p := <-q:
				fn(p)
			}
		}
	}()
	send = func(payload []byte) {
		cp := append([]byte(nil), payload...)
		for {
			select {
			case <-stopped:
				return
			case q <- cp:
				return
			default:
			}
			// Queue full: drop oldest and retry. Receiving from a buffered
			// channel needs no concurrent consumer, so this pops a queued
			// payload even while fn is still running — guaranteeing forward
			// progress (no busy-spin) and the drop-oldest contract. stopped
			// is re-checked every iteration.
			select {
			case <-q:
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
	// does it drain queued payloads — this is a lossy lane, so dropping the
	// few still-queued messages on unsubscribe is the intended behavior.
	stop = func() {
		once.Do(func() { close(stopped) })
	}
	return send, stop
}
