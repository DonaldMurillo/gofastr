package acp

import (
	"errors"
	"io"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// A writer that drops a byte without an error must not count as a sent
// frame, matching moduleproto's Codec.WriteFrame.
func TestWriteFrameReportsShortWrite(t *testing.T) {
	if err := newConn(shortWriter{}).writeFrame(map[string]int{"a": 1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeFrame = %v, want io.ErrShortWrite", err)
	}
}

// partialWriter stores one byte and fails, then accepts everything.
type partialWriter struct{ calls int }

func (p *partialWriter) Write(b []byte) (int, error) {
	p.calls++
	if p.calls == 1 {
		return 1, errors.New("disk full")
	}
	return len(b), nil
}

// After a frame is cut short the stream is out of step, so every later
// frame fails instead of landing in the middle of the broken one.
func TestPartialWriteLatches(t *testing.T) {
	w := &partialWriter{}
	c := newConn(w)
	if c.writeFrame(map[string]int{"a": 1}) == nil {
		t.Fatal("partial write succeeded")
	}
	if c.writeFrame(map[string]int{"b": 2}) == nil || w.calls != 1 {
		t.Fatalf("frame written after a partial one (writes=%d)", w.calls)
	}
}
