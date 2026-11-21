package log

import (
	"bytes"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// RingSink is an in-memory Sink that keeps the most recent N log entries
// in a fixed-size circular buffer.
type RingSink struct {
	mu     sync.Mutex
	buf    [][]byte
	cap    int
	head   int
	full   bool
	closed bool
}

// NewRingSink returns a RingSink that retains the last cap entries.
func NewRingSink(cap int) *RingSink {
	if cap <= 0 {
		cap = 1000
	}
	return &RingSink{
		buf: make([][]byte, cap),
		cap: cap,
	}
}

// Write stores a copy of the entry in the ring.
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

// Cap returns the ring's fixed capacity.
func (r *RingSink) Cap() int { return r.cap }

// Close marks the sink closed.
func (r *RingSink) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

// Snapshot returns a copy of the current ring contents in chronological
// order (oldest first).
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
// entry to a map[string]any.
func (r *RingSink) SnapshotDecoded() []map[string]any {
	raw := r.Snapshot()
	out := make([]map[string]any, 0, len(raw))
	for _, line := range raw {
		var m map[string]any
		if err := jsonexact.Unmarshal(bytes.TrimSpace(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}
