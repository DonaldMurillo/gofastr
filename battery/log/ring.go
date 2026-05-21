package log

import (
	"bytes"
	"encoding/json"
	"sync"
)

// RingSink is an in-memory Sink that keeps the most recent N log
// entries in a fixed-size circular buffer. It's the backing store for
// the log MCP tools — agents query it via log_recent / log_filter for
// fast, structured live debugging.
//
// Concurrency: safe for concurrent Write and Snapshot. Snapshot returns
// a copy so the caller can iterate without holding the mutex.
//
// Memory bound: each entry is a []byte (the JSON line from the fanout).
// At cap=1000 and ~500-byte entries the worst-case footprint is ~500 KiB.
// Tune the cap for apps that emit very large entries.
type RingSink struct {
	mu     sync.Mutex
	buf    [][]byte
	cap    int
	head   int  // index of next write
	full   bool // wrapped at least once
	closed bool
}

// NewRingSink returns a RingSink that retains the last cap entries.
// A cap <= 0 defaults to 1000.
func NewRingSink(cap int) *RingSink {
	if cap <= 0 {
		cap = 1000
	}
	return &RingSink{
		buf: make([][]byte, cap),
		cap: cap,
	}
}

// Write stores a copy of the entry in the ring. Returns ErrSinkClosed
// after Close has run — consistent with the other sinks so the fanout's
// post-Stop drop counter advances correctly.
func (r *RingSink) Write(entry []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrSinkClosed
	}
	cp := make([]byte, len(entry))
	copy(cp, entry)
	r.buf[r.head] = cp
	r.head = (r.head + 1) % r.cap
	if r.head == 0 {
		r.full = true
	}
	return nil
}

// Close marks the sink closed. Subsequent Write calls return
// ErrSinkClosed. Idempotent.
func (r *RingSink) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

// Snapshot returns a copy of the current ring contents in chronological
// order (oldest first). Each entry is a []byte JSON line as written by
// the fanout's encoder. Safe to call concurrently with Write.
func (r *RingSink) Snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.cap
	start := 0
	if !r.full {
		n = r.head
	} else {
		start = r.head
	}
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		src := r.buf[(start+i)%r.cap]
		cp := make([]byte, len(src))
		copy(cp, src)
		out[i] = cp
	}
	return out
}

// SnapshotDecoded is a convenience wrapper that unmarshals each ring
// entry to a map[string]any. Returned in chronological order. Entries
// that fail to decode are skipped (defensive — the fanout writes valid
// JSON, but custom sinks composed into a RingSink might not).
func (r *RingSink) SnapshotDecoded() []map[string]any {
	raw := r.Snapshot()
	out := make([]map[string]any, 0, len(raw))
	for _, line := range raw {
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}
