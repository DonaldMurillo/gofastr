package log

import (
	"io"
	"os"
	"sync"
)

// JSONOpts configures a JSON sink. The zero value is ready to use:
// writes to os.Stdout.
type JSONOpts struct {
	// Writer is the destination. Nil defaults to os.Stdout — the
	// 12-factor container pattern where the platform (Docker json-file,
	// Kubernetes, Cloud Run, …) captures stdout and ships it.
	Writer io.Writer
}

// jsonSink is the concrete Sink for line-oriented JSON streams.
// Entries pass through byte-for-byte: the fanout handler already
// serialized them with slog's JSON encoder, so there is nothing to
// re-encode — and re-encoding would only reorder keys.
type jsonSink struct {
	mu     sync.Mutex
	w      io.Writer
	closed bool
}

// JSONSink returns a Sink that writes each entry as one JSON object per
// line to opts.Writer (os.Stdout when nil).
//
// The output is the fanout's encoding untouched (see the Sink.Write
// contract): standard slog JSON with `time`, `level`, `msg`, and the
// entry's attrs, one object per line, '\n'-terminated. Pass it to
// anything that reads JSON-lines — Docker's json-file driver, Fluentd,
// Vector, Loki's promtail, `kubectl logs`.
//
// Each entry is written with a single Write call, so a log collector
// draining the stream never observes a torn line even under load.
//
// Concurrency: safe for concurrent use. Write serializes on a mutex so
// concurrent entries don't interleave — consistent with the other
// sinks. After Close, Write returns ErrSinkClosed; Close never closes
// the underlying writer (the sink doesn't own os.Stdout) and is
// idempotent.
func JSONSink(opts JSONOpts) Sink {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	return &jsonSink{w: w}
}

// Write emits one '\n'-terminated JSON line. The entry bytes are
// copied into the line buffer rather than appended in place — callers
// that reuse their slice keep their backing array intact.
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

// Close marks the sink closed. It does not close the underlying
// writer — os.Stdout is process-owned.
func (s *jsonSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}
