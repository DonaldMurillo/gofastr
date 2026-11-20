package moduleproto

import (
	"io"
	"sync"
)

// DefaultRingSinkBytes is RingSink's default capacity, 64 KiB.
const DefaultRingSinkBytes = 64 * 1024

// RingSink keeps the last cap bytes written to it, for a child's stderr.
// Write never blocks or fails; it is safe for concurrent use.
type RingSink struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

// NewRingSink keeps the last capBytes bytes (0 = DefaultRingSinkBytes).
func NewRingSink(capBytes int) *RingSink {
	if capBytes <= 0 {
		capBytes = DefaultRingSinkBytes
	}
	return &RingSink{cap: capBytes}
}

// Write appends p, dropping the oldest bytes past the capacity.
func (r *RingSink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
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

// Drain copies src into the sink until EOF, returning nil, or another
// read error, which it returns.
func (r *RingSink) Drain(src io.Reader) error {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
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
