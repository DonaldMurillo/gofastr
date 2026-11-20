package fanout

import "context"

// Fanout carries lossy real-time messages between replicas; payloads are UTF-8.
type Fanout interface {
	// Publish broadcasts payload to every subscriber of topic on every node.
	Publish(ctx context.Context, topic string, payload []byte) error

	// Subscribe registers fn, which must not block, for topic on every node.
	Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error)
}
