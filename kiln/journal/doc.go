// Package journal is the append-only event log that backs every Kiln
// session. A single log captures both world mutations and the chat
// transcript that produced them, so replay reconstructs the entire
// session — world, chat, and pending plans — from a JSONL file.
//
// The unified-log design lets undo, freeze, and audit share one source
// of truth. Each Entry has a Kind discriminator; world_edit entries
// additionally carry an Op naming the specific mutation. New kinds and
// ops are added by registering an apply function — Replay does no
// reflection.
package journal
