package log

import (
	"io"
	"os"
	"sync"
)

// JSONOpts configures a JSON sink.
type JSONOpts struct {
	// Writer is the destination.
	Writer io.Writer
}

type jsonSink struct {
	mu     sync.Mutex
	w      io.Writer
	closed bool
}

// JSONSink returns a Sink that writes each entry as one JSON object per
// line to opts.Writer (os.Stdout when nil).
func JSONSink(opts JSONOpts) Sink {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	return &jsonSink{w: w}
}

// Write emits one '\n'-terminated JSON line.
func (s *jsonSink) Write(entry []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSinkClosed
	}
	line := make([]byte, 0, len(entry)+1)
	line = append(line, entry...)
	line = append(line, '\n')
	_, err := s.w.Write(line)
	return err
}

// Close marks the sink closed.
func (s *jsonSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}
