package stream

import (
	"io"
	"net/http"
	"sync"
)

// ChunkedWriter writes raw chunks to an http.ResponseWriter with flush.
type ChunkedWriter struct {
	w          http.ResponseWriter
	mu         sync.Mutex
	controller *http.ResponseController
}

// NewChunkedWriter wraps w. It panics if w is not an http.Flusher.
func NewChunkedWriter(w http.ResponseWriter) *ChunkedWriter {
	_, ok := w.(http.Flusher)
	if !ok {
		panic("stream: http.ResponseWriter does not implement http.Flusher")
	}
	return &ChunkedWriter{
		w:          w,
		controller: http.NewResponseController(w),
	}
}

// WriteChunk writes data to the underlying response writer and flushes.
func (c *ChunkedWriter) WriteChunk(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return c.controller.Flush()
}

// Close performs a final flush. It is safe to call multiple times.
func (c *ChunkedWriter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.controller.Flush()
}
