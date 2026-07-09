package fanout

import (
	"context"
	"fmt"
)

// RedisPubSub is the minimal Redis surface [NewRedis] needs. No Redis library
// is imported; implement this with your preferred client (go-redis, redigo,
// …). See the package doc for a go-redis adapter example.
//
// Subscribe must invoke fn for every message published on channel by ANY
// client, including other processes/replicas. The returned cancel stops
// delivery and releases the subscription.
type RedisPubSub interface {
	Publish(ctx context.Context, channel string, payload []byte) error
	Subscribe(ctx context.Context, channel string, fn func(payload []byte)) (cancel func(), err error)
}

// redisFanout adapts a [RedisPubSub] to the [Fanout] interface. One Redis
// channel backs each fanout topic (the topic string is used verbatim as the
// channel name).
type redisFanout struct {
	client RedisPubSub
}

// NewRedis returns a [Fanout] backed by the supplied [RedisPubSub]. The
// caller owns the Redis client and adapts it to RedisPubSub.
func NewRedis(client RedisPubSub) Fanout {
	return &redisFanout{client: client}
}

// Publish broadcasts payload to the topic's Redis channel.
func (r *redisFanout) Publish(ctx context.Context, topic string, payload []byte) error {
	if err := r.client.Publish(ctx, topic, payload); err != nil {
		return fmt.Errorf("fanout: redis publish %q: %w", topic, err)
	}
	return nil
}

// Subscribe registers fn for the topic's Redis channel. The user callback is
// wrapped in a [SubscriberQueue] so it runs on a dedicated goroutine with a
// bounded, drop-oldest queue — preserving the per-subscriber contract the
// [Fanout] interface promises, so one slow subscriber cannot stall the Redis
// reader goroutine (which fans out to every subscriber on this topic).
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
