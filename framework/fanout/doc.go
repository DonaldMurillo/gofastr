// Package fanout provides a Postgres-backed implementation of
// [core/fanout.Fanout] using LISTEN/NOTIFY.
//
// PostgresFanout uses ONE channel, named after the fallback table
// ("gofastr_fanout_msgs" by default, so two fanouts with different tables on
// one database are fully isolated); the fanout topic travels inside the
// NOTIFY payload so dynamic topic strings never need to be validated as PG
// channel identifiers. Payloads that fit under the
// ~7000-byte inline threshold (leaving margin below Postgres's 8000-byte
// NOTIFY limit) are delivered inline; larger payloads are INSERTed into a
// fallback table and a short "t:<id>" pointer is NOTIFY'd, with receivers
// SELECTing the row by id.
//
// Delivery is lossy best-effort, like every Fanout: pq.Listener reconnects
// automatically after a connection gap, but messages published during the gap
// are lost (the durable lane is the transactional outbox's job).
package fanout
