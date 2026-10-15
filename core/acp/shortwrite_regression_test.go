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
