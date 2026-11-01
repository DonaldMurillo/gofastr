package fanout

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// publishTimeout bounds each backend Publish issued by a PublishQueue. The
// lane is lossy best-effort; a backend stalled past this is treated as a
// dropped message, never as backpressure on the caller.
const publishTimeout = 5 * time.Second

// ErrPublishDropped is what a PublishQueueReporting callback receives for
// a payload the queue dropped to make room for a newer one: the backend
// was too slow to take everything sent.
var ErrPublishDropped = errors.New("fanout: publish queue full; message dropped")

// PublishQueue returns a non-blocking send for mirroring messages to a
// [Fanout] from hot paths (HTTP handlers, event emitters). Publishing to a
// real backend is a network/DB round-trip; calling [Fanout.Publish] inline
// would hand every caller an unbounded stall on a slow or wedged backend.
//
// send enqueues into a bounded, drop-oldest queue drained by one dedicated
// goroutine that publishes each payload under a fixed per-publish deadline;
// it never blocks and never returns an error. Publish failures are logged
// at Debug (lossy lane: the durable path is the outbox's job). After stop,
// send is a silent no-op, and stop is safe to call multiple times. A caller
// that must act on a lost payload uses [PublishQueueReporting].
//
// stop drops what is still queued and returns once a publish already in
// flight has finished, which the per-publish deadline bounds. A caller
// that sends a final message directly after stop (a presence goodbye)
// can rely on no earlier queued message landing after it. stop must not
// be called while holding a lock the backend's Publish takes.
//
// depth <= 0 selects the default queue depth.
func PublishQueue(f Fanout, topic string, depth int) (send func([]byte), stop func()) {
	sendReporting, stop := PublishQueueReporting(f, topic, depth)
	return func(payload []byte) { sendReporting(payload, nil) }, stop
}

// PublishQueueReporting is [PublishQueue] whose send takes a per-payload
// failed callback. failed runs once for a payload that was not published:
// with the backend's error (a timeout included) on the queue's goroutine,
// or with [ErrPublishDropped] on the sending goroutine when the queue
// dropped it to make room. A nil failed reports nothing. failed must not
// block; a panic in it is logged and contained. Payloads still queued at
// stop are dropped without a report: the owner is closing the lane.
func PublishQueueReporting(f Fanout, topic string, depth int) (send func(payload []byte, failed func(error)), stop func()) {
	type item struct {
		payload []byte
		failed  func(error)
	}
	var (
		mu       sync.Mutex
		closed   bool
		inflight sync.WaitGroup
	)
	sendItem, stopQueue := dropOldestQueue(func(it item) {
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
		if err := publishSafely(f, topic, it.payload); err != nil {
			slog.Default().Debug("fanout: publish failed; message dropped (lossy lane)",
				"topic", topic, "err", err)
			reportFailed(it.failed, err)
		}
	}, depth, func(it item) {
		reportFailed(it.failed, ErrPublishDropped)
	})
	send = func(payload []byte, failed func(error)) {
		sendItem(item{payload: append([]byte(nil), payload...), failed: failed})
	}
	stop = func() {
		stopQueue()
		mu.Lock()
		closed = true
		mu.Unlock()
		inflight.Wait()
	}
	return send, stop
}

// errPublishPanicked is what a failed callback receives when the backend's
// Publish panicked.
var errPublishPanicked = errors.New("fanout: publish panicked; message dropped")

// publishSafely runs one backend Publish under the queue's deadline with
// panic isolation. The drainer is a goroutine the framework owns, so a
// panicking backend (a host Fanout bug, a closed client) would otherwise
// take the whole replica down on the next mirrored message. The payload is
// dropped and reported like any other failed publish.
func publishSafely(f Fanout, topic string, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: publish panicked; message dropped",
				"topic", topic, slog.Any("panic", textsafe.Recovered(r)))
			err = errPublishPanicked
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	return f.Publish(ctx, topic, payload)
}

// reportFailed runs a caller's failed callback with panic isolation: it
// runs on the queue's goroutine, which has no caller frame to unwind into.
func reportFailed(failed func(error), err error) {
	if failed == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: publish failure callback panicked",
				slog.Any("panic", textsafe.Recovered(r)))
		}
	}()
	failed(err)
}
