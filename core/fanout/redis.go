package fanout

import (
	"context"
	"fmt"
)

// RedisPubSub is the Redis client surface [NewRedis] needs.
type RedisPubSub interface {
	Publish(ctx context.Context, channel string, payload []byte) error
	Subscribe(ctx context.Context, channel string, fn func(payload []byte)) (cancel func(), err error)
}

type redisFanout struct {
	client RedisPubSub
}

// NewRedis returns a [Fanout] on the Redis channel named by each topic.
func NewRedis(client RedisPubSub) Fanout {
	return &redisFanout{client: client}
}

func (r *redisFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	if err := r.client.Publish(ctx, topic, payload); err != nil {
		return fmt.Errorf("fanout: redis publish %q: %w", topic, err)
	}
	return nil
}

func (r *redisFanout) Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error) {
	send, stop := SubscriberQueue(fn, 0)
	c, err := r.client.Subscribe(context.Background(), topic, send)
	if err != nil {
		stop()
		return nil, fmt.Errorf("fanout: redis subscribe %q: %w", topic, err)
	}
	return func() {
		c()
		stop()
	}, nil
}
