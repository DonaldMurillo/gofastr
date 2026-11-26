package stream

import (
	"errors"
	"io"
	"net/http/httptest"
	"testing"
)

type chunkResponse struct {
	*httptest.ResponseRecorder
	short    bool
	flushErr error
}

func (r *chunkResponse) Write(p []byte) (int, error) {
	if r.short {
		return len(p) - 1, nil
	}
	return r.ResponseRecorder.Write(p)
}

func (r *chunkResponse) FlushError() error { return r.flushErr }

func TestChunkedWriterReportsShortWrite(t *testing.T) {
	c := NewChunkedWriter(&chunkResponse{ResponseRecorder: httptest.NewRecorder(), short: true})
	if err := c.WriteChunk([]byte("chunk")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v", err)
	}
}

func TestChunkedWriterReportsFlushErrors(t *testing.T) {
	want := errors.New("flush failed")
	c := NewChunkedWriter(&chunkResponse{ResponseRecorder: httptest.NewRecorder(), flushErr: want})
	if err := c.WriteChunk([]byte("chunk")); !errors.Is(err, want) {
		t.Errorf("write error = %v", err)
	}
	if err := c.Close(); !errors.Is(err, want) {
		t.Errorf("close error = %v", err)
	}
}
