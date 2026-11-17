// Package outbox is a transactional outbox: Append stages an event row in
// the caller's transaction, and the relay StartRelay runs delivers each
// committed row to every consumer declared with Consume, at least once,
// retrying and dead-lettering each consumer on its own.
package outbox
