package stream

import (
	"net/http"
	"sync"
)

// ChunkedWriter writes raw chunks to an http.ResponseWriter with flush.
type ChunkedWriter struct {
	w     http.ResponseWriter
	mu    sync.Mutex
	flush http.Flusher
}

// NewChunkedWriter creates a ChunkedWriter wrapping w.
// It panics if w does not implement http.Flusher.
func NewChunkedWriter(w http.ResponseWriter) *ChunkedWriter {
	flush, ok := w.(http.Flusher)
	if !ok {
		panic("stream: http.ResponseWriter does not implement http.Flusher")
	}
	return &ChunkedWriter{
		w:     w,
		flush: flush,
	}
}

// WriteChunk writes data to the underlying response writer and flushes.
func (c *ChunkedWriter) WriteChunk(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.w.Write(data)
	c.flush.Flush()
	return err
}

// Close performs a final flush. It is safe to call multiple times.
func (c *ChunkedWriter) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flush.Flush()
	return nil
}
