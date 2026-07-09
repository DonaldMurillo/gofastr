// Package fanout carries real-time messages between replicas.
//
// GoFastr's real-time surfaces — [event.EventBus], [island.Manager], and
// [stream.SSEBroker] — are per-process by default. A write on replica A
// notifies only the browsers connected to A. Fanout is the seam that lets
// those surfaces broadcast across replicas.
//
// # Lossy best-effort by design
//
// This is the real-time lane. Implementations are permitted to drop a message
// published while a subscriber is disconnected, reconnecting, or queue-full.
// Durable, at-least-once delivery is the transactional outbox's job
// (framework/outbox — see "Two delivery lanes"). The two lanes are disjoint:
// fanout never participates in the durable path.
//
// # Loop guard
//
// Every node publishes its messages wrapped in an envelope that carries the
// originator's node id ([Wrap]). Receivers drop any message whose node id
// matches their own ([Unwrap]) so a broadcast is not re-broadcast. The
// integrations (bus bridge, island manager, SSE broker) apply this guard on
// receive; the Fanout itself is node-agnostic.
//
// # Backends
//
//   - [InProcess] — an in-memory pub/sub. Its primary purpose is tests: wiring
//     two buses / two island managers / two brokers to one InProcess simulates
//     two replicas inside a single test binary.
//
//   - [NewRedis] — a thin adapter over a user-supplied [RedisPubSub]. No Redis
//     library is imported; adapt go-redis (or redigo) in ~15 lines:
//
//     // go-redis adapter
//     type goRedisPubSub struct{ c *redis.Client }
//
//     func (a goRedisPubSub) Publish(ctx context.Context, ch string, p []byte) error {
//     return a.c.Publish(ctx, ch, p).Err()
//     }
//     func (a goRedisPubSub) Subscribe(ctx context.Context, ch string, fn func([]byte)) (func(), error) {
//     sub := a.c.Subscribe(ctx, ch)
//     ch2 := sub.Channel(redis.WithChannelSize(64))
//     done := make(chan struct{})
//     go func() { defer close(done); for m := range ch2 { fn([]byte(m.Payload)) } }()
//     return func() { _ = sub.Close(); <-done }, nil
//     }
//     fanout.NewRedis(goRedisPubSub{c: client})
//
// # Trusted transport
//
// The fanout transport is trusted input: write access to the underlying
// channel (a Postgres NOTIFY channel, a Redis pub/sub channel, …) equals
// event-injection into every replica. Payloads are not authenticated — a
// forged envelope published to the channel is re-emitted on every bus that
// subscribes to it. Secure the channel (network isolation, DB/Redis
// credentials) rather than relying on the fanout to reject malicious input.
//
// For a Postgres LISTEN/NOTIFY backend see package framework/fanout.
package fanout
