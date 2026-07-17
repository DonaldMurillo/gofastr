package moduleproto

import (
	"io"
	"sync"
)

// DefaultRingSinkBytes is the default capacity of [RingSink] (design §4.2:
// "bounded ring sink so child logs are observable without unbounded memory",
// 64 KiB mirroring the stderr observability budget).
const DefaultRingSinkBytes = 64 * 1024

// RingSink is a bounded, non-blocking byte sink that retains the last N bytes
// written to it (design §4.2). It wraps the child's stderr so the host can
// observe recent child log output without allowing an unbounded log to exhaust
// memory. It replaces mcpclient's drainStderr's `io.Copy(io.Discard, …)`.
//
// Properties:
//
//   - NEVER blocks the writer (the child's stderr drain goroutine). Write
//     always succeeds immediately and returns len(p), regardless of how full
//     the ring is.
//   - Retains at most cap bytes. When the ring would overflow, the OLDEST
//     bytes are evicted. [Tail] returns the retained slice in write order.
//   - Safe for concurrent Write and Tail.
//
// Implementation: a single growable []byte guarded by a mutex, trimmed to the
// last cap bytes on each write. The trim is O(remaining) but stderr is
// low-volume relative to the protocol path, so a proper circular buffer would
// add complexity without measurable benefit. If the stderr path ever carries
// high volume, swap in a circular buffer keeping the same API.
type RingSink struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

// NewRingSink constructs a RingSink retaining the last capBytes bytes. If
// capBytes <= 0, [DefaultRingSinkBytes] is used.
func NewRingSink(capBytes int) *RingSink {
	if capBytes <= 0 {
		capBytes = DefaultRingSinkBytes
	}
	return &RingSink{cap: capBytes}
}

// Write appends p to the ring, evicting the oldest bytes if necessary so the
// retained slice never exceeds cap. It never blocks and never returns an error.
// Implements [io.Writer].
func (r *RingSink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		// Keep the most recent cap bytes.
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.cap:]...)
	}
	return len(p), nil
}

// Tail returns a snapshot of the retained bytes in write order. The returned
// slice is a copy; the caller may mutate it freely.
func (r *RingSink) Tail() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}

// Len returns the number of bytes currently retained.
func (r *RingSink) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// Cap returns the configured capacity in bytes.
func (r *RingSink) Cap() int { return r.cap }

// Reset clears the retained bytes.
func (r *RingSink) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = r.buf[:0]
}

// Drain copies everything from src into the sink until src returns EOF (or a
// non-EOF read error, which is returned). It is intended to run in its own
// goroutine wrapping the child's stderr; it blocks on src.Read, NEVER on the
// ring (Write never blocks).
//
// On a non-EOF error from src, Drain returns that error so the caller can log
// it; the sink retains whatever was copied up to that point.
func (r *RingSink) Drain(src io.Reader) error {
	// 4 KiB read buffer matches typical stderr line sizes; this is the
	// child's log path, not the protocol path.
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			// Write to the ring cannot fail.
			_, _ = r.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
