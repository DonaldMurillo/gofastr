// Package outbox implements a transactional outbox for reliable event
// delivery.
//
// The in-process [event.EventBus] emits after the database transaction
// commits. If the process crashes in the gap between commit and emit, the
// event is lost. The outbox closes that gap: Append writes the event row
// inside the caller's transaction (it commits or rolls back with the
// business write), and a background Relay publishes committed rows to the
// bus with at-least-once semantics.
//
// # At-least-once delivery
//
// The Relay claims a batch of pending rows, calls [event.EventBus.Emit]
// synchronously, and marks the row dispatched only after Emit succeeds. A
// crash between Emit and the mark leaves the row claimable; on restart it
// is delivered again. Consumers MUST be idempotent and deduplicate by
// [event.Event].ID, which the Relay stamps from the outbox row's primary
// key. Events emitted directly via Emit/EmitAsync carry an empty ID and
// have no durable identity.
//
// # Multi-replica safety
//
// The claim takes a lease (the claimed_until column). A Relay that dies
// mid-batch holds its rows until the lease expires; after expiry another
// Relay — or the same process after a restart — reclaims and re-delivers
// them. This lets several replicas run a Relay against one shared table
// without double-processing (modulo the at-least-once caveat above).
//
// # Layering
//
// outbox is an L3 leaf package with two deliberate intra-L3 edges:
// outbox → event (publishes Events) and outbox → db (uses db.Executor so
// Append participates in the caller's transaction). The precedent is
// slowquery → db.
package outbox
