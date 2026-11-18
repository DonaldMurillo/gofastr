// Package fanout implements [core/fanout.Fanout] on Postgres LISTEN/NOTIFY
// with NewPostgres. Delivery is lossy: the durable lane is the outbox's.
package fanout
