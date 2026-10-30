package fanout

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// publishTimeout bounds each backend Publish issued by a PublishQueue. The
// lane is lossy best-effort; a backend stalled past this is treated as a
// dropped message, never as backpressure on the caller.
const publishTimeout = 5 * time.Second

// PublishQueue returns a non-blocking send for mirroring messages to a
// [Fanout] from hot paths (HTTP handlers, event emitters). Publishing to a
// real backend is a network/DB round-trip; calling [Fanout.Publish] inline
// would hand every caller an unbounded stall on a slow or wedged backend.
//
// send enqueues into a bounded, drop-oldest queue drained by one dedicated
// goroutine that publishes each payload under a fixed per-publish deadline;
// it never blocks and never returns an error. Publish failures are logged
// at Debug (lossy lane: the durable path is the outbox's job). After stop,
// send is a silent no-op, and stop is safe to call multiple times.
//
// stop drops what is still queued and returns once a publish already in
// flight has finished, which the per-publish deadline bounds. A caller
// that sends a final message directly after stop (a presence goodbye)
// can rely on no earlier queued message landing after it. stop must not
// be called while holding a lock the backend's Publish takes.
//
// depth <= 0 selects the default queue depth.
func PublishQueue(f Fanout, topic string, depth int) (send func([]byte), stop func()) {
	var (
		mu       sync.Mutex
		closed   bool
		inflight sync.WaitGroup
	)
	send, stopQueue := SubscriberQueue(func(payload []byte) {
		// The drainer can take a payload in the same instant stop closes
		// the queue; the gate keeps that publish from starting after stop
		// returned.
		mu.Lock()
		if closed {
			mu.Unlock()
			return
		}
		inflight.Add(1)
		mu.Unlock()
		defer inflight.Done()
		ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
		defer cancel()
		if err := f.Publish(ctx, topic, payload); err != nil {
			slog.Default().Debug("fanout: publish failed; message dropped (lossy lane)",
				"topic", topic, "err", err)
		}
	}, depth)
	stop = func() {
		stopQueue()
		mu.Lock()
		closed = true
		mu.Unlock()
		inflight.Wait()
	}
	return send, stop
}
