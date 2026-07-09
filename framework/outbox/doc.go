// Package outbox implements a transactional outbox for reliable event
// delivery to declared durable consumers.
//
// The in-process [event.EventBus] emits after the database transaction
// commits. If the process crashes in the gap between commit and emit, the
// event is lost. The outbox closes that gap: Append writes the event row
// inside the caller's transaction (it commits or rolls back with the
// business write), and a background Relay delivers each committed row to
// every declared durable consumer with at-least-once semantics.
//
// # Two delivery lanes
//
// Delivery is split across two disjoint lanes, so neither duplicates the
// other:
//
//   - Real-time lane (best-effort, ephemeral): the live event bus is
//     notified post-commit by the caller (e.g. crud.EmitEvent), feeding
//     SSE streams and ephemeral On/Subscribe handlers. Lossy by design.
//   - Durable lane (tracked per consumer): the Relay delivers each row to
//     the consumers declared via [Outbox.Consume]. The Relay does NOT
//     touch the live bus.
//
// # Per-consumer delivery & sibling isolation
//
// Each (parent row, declared consumer) pair has its own delivery row in
// event_outbox_delivery. A consumer that errors or panics is retried with
// exponential backoff and eventually dead-lettered INDEPENDENTLY of its
// siblings — one broken consumer never blocks another or fails the whole
// row. A consumer that is removed (no handler on any replica) has its
// deliveries abandoned once they age past the handler grace, so it can't
// block completion. A parent row is marked dispatched once it has no
// pending deliveries left (all dispatched/dead/abandoned); it may complete
// with some deliveries dead. [Outbox.Replay] / [Outbox.ReplayConsumer]
// resurrect dead or abandoned deliveries.
//
// # At-least-once delivery
//
// The Relay claims a batch of pending deliveries, invokes the consumer's
// handler, and marks the delivery dispatched only after the handler
// returns nil. A crash between the two re-delivers (consumers dedup by
// [event.Event].ID, which the Relay stamps from the outbox row id).
//
// # Multi-replica safety
//
// The claim takes a lease (the claimed_until column) at the delivery
// grain. A Relay that dies mid-batch holds only its claimed deliveries
// until the lease expires; after expiry another Relay — or the same
// process after a restart — reclaims and re-delivers them.
//
// # Layering
//
// outbox is an L3 leaf package with two deliberate intra-L3 edges:
// outbox → event (uses event.EventHandler / event.Event) and outbox → db
// (uses db.Executor so Append participates in the caller's transaction).
// The precedent is slowquery → db.
package outbox
