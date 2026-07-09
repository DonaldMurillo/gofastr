package fanout

import (
	"context"
	"log/slog"
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
// it never blocks and never returns an error — publish failures are logged
// at Debug (lossy lane: the durable path is the outbox's job). After stop,
// send is a silent no-op; stop is prompt and safe to call multiple times.
//
// depth <= 0 selects the default queue depth.
func PublishQueue(f Fanout, topic string, depth int) (send func([]byte), stop func()) {
	return SubscriberQueue(func(payload []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
		defer cancel()
		if err := f.Publish(ctx, topic, payload); err != nil {
			slog.Default().Debug("fanout: publish failed; message dropped (lossy lane)",
				"topic", topic, "err", err)
		}
	}, depth)
}
