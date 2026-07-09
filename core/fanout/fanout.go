package fanout

import "context"

// Fanout carries real-time messages between replicas. Implementations are
// lossy best-effort: a message published while a subscriber is disconnected,
// reconnecting, or queue-full is gone. Durable delivery belongs to the
// transactional outbox (framework/outbox).
//
// Payloads must be valid UTF-8: framework payloads are JSON envelopes, and a
// backend may reject invalid UTF-8 (e.g. the Postgres backend, which would
// otherwise silently corrupt such bytes via U+FFFD substitution). The fanout
// transport is trusted input — write access to the underlying channel equals
// event-injection into every replica (forged payloads are not authenticated).
type Fanout interface {
	// Publish broadcasts payload to all subscribers of topic on every node.
	// It is best-effort: a slow or absent subscriber is dropped, not
	// backpressured (unless the backend cannot help it).
	Publish(ctx context.Context, topic string, payload []byte) error

	// Subscribe registers fn for every payload published on topic by any
	// node. fn must not block; backends invoke it on a dedicated goroutine
	// with a bounded queue and drop oldest on overflow (mirroring
	// [stream.SSEBroker]). The returned cancel unregisters fn; safe to call
	// multiple times.
	Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error)
}
