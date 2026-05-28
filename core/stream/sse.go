package stream

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// SSEWriter writes Server-Sent Events to an http.ResponseWriter.
//
// It automatically sets the required headers (Content-Type, Cache-Control,
// Connection) on first write and flushes after every event.
type SSEWriter struct {
	w      http.ResponseWriter
	mu     sync.Mutex
	init   bool // true after headers have been written
	flush  http.Flusher
	nextID string // queued id to emit before the next event
}

// NewSSEWriter creates an SSEWriter wrapping w.
// It panics if w does not implement http.Flusher.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flush, ok := w.(http.Flusher)
	if !ok {
		panic("stream: http.ResponseWriter does not implement http.Flusher")
	}
	return &SSEWriter{
		w:     w,
		flush: flush,
	}
}

// ensureHeaders sets Content-Type, Cache-Control, and Connection headers
// exactly once, before the first byte of the body is written.
func (s *SSEWriter) ensureHeaders() {
	if s.init {
		return
	}
	s.init = true
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
}

// Flush sends any buffered data to the client immediately.
func (s *SSEWriter) Flush() {
	s.flush.Flush()
}

// SetRetry writes the "retry:" field, telling the client how many
// milliseconds to wait before reconnecting. Non-positive values are
// dropped: `retry: 0` tells the client to reconnect with zero delay,
// which spins into a reconnect storm — accidental DoS amplifier.
func (s *SSEWriter) SetRetry(seconds int) {
	if seconds <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()
	fmt.Fprintf(s.w, "retry: %d\n", seconds)
}

// SetID queues an "id:" field to be emitted before the next event.
func (s *SSEWriter) SetID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID = id
}

// WriteEvent writes a named SSE event:
//
//	event: <name>
//	data: <data>
//
// followed by a blank line and a flush.
//
// CR/LF characters in the event name are stripped — an event name may
// only occupy a single SSE field line. A caller-supplied newline would
// otherwise terminate the field and let following bytes appear as
// arbitrary SSE directives.
func (s *SSEWriter) WriteEvent(event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()

	event = stripSSEControlChars(event)

	var b strings.Builder

	// queued id
	if s.nextID != "" {
		b.WriteString("id: ")
		b.WriteString(stripSSEControlChars(s.nextID))
		b.WriteByte('\n')
		s.nextID = ""
	}

	b.WriteString("event: ")
	b.WriteString(event)
	b.WriteByte('\n')

	// multi-line data — strip CR/NUL per the WHATWG SSE parser, which
	// terminates a field on CR, LF, or CRLF. A `data: foo\rbar` field
	// would otherwise split into two values on Windows EventSource impls.
	for _, line := range strings.Split(scrubSSEDataLines(data), "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n') // blank line terminates event

	_, err := s.w.Write([]byte(b.String()))
	s.flush.Flush()
	return err
}

// WriteData writes an anonymous SSE event (type defaults to "message"):
//
//	data: <data>
//
// followed by a blank line and a flush.
func (s *SSEWriter) WriteData(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()

	var b strings.Builder

	// queued id
	if s.nextID != "" {
		b.WriteString("id: ")
		b.WriteString(stripSSEControlChars(s.nextID))
		b.WriteByte('\n')
		s.nextID = ""
	}

	for _, line := range strings.Split(scrubSSEDataLines(data), "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	_, err := s.w.Write([]byte(b.String()))
	s.flush.Flush()
	return err
}

// scrubSSEDataLines strips CR and NUL bytes from a multi-line data
// payload before \n-splitting. NUL would terminate the field on legacy
// clients; CR terminates the field on WHATWG-spec parsers.
func scrubSSEDataLines(s string) string {
	if !strings.ContainsAny(s, "\r\x00") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == 0 {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// WriteComment writes an SSE comment (keepalive):
//
//	: <comment>
//
// followed by a blank line and a flush. The comment is truncated at the
// first CR/LF so a caller can't terminate the comment line and inject
// arbitrary SSE fields ("event: …", "data: …", …) below it.
func (s *SSEWriter) WriteComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()

	comment = stripSSEControlChars(comment)

	var b strings.Builder
	b.WriteString(": ")
	b.WriteString(comment)
	b.WriteString("\n\n")

	_, err := s.w.Write([]byte(b.String()))
	s.flush.Flush()
	return err
}

// stripSSEControlChars truncates at the first CR, LF or NUL — those
// are the only characters that can break out of a single SSE field,
// and once one appears the rest of the input is treated as adversarial
// (an injected directive or header). We don't merely *delete* the
// control char because the surrounding bytes can recombine into the
// payload we're trying to neutralise (e.g. "X\r\nSet-Cookie:" →
// "XSet-Cookie:" still leaks the literal header).
func stripSSEControlChars(s string) string {
	if i := strings.IndexAny(s, "\r\n\x00"); i >= 0 {
		return s[:i]
	}
	return s
}

// WriteMessage is a convenience for writing a Message event.
func (s *SSEWriter) WriteMessage(data string) error {
	return s.WriteEvent("message", data)
}

// WriteError is a convenience for writing an Error event.
func (s *SSEWriter) WriteError(message string) error {
	return s.WriteEvent("error", message)
}

// WriteDone sends the terminal "[DONE]" sentinel.
func (s *SSEWriter) WriteDone() error {
	return s.WriteEvent("done", "[DONE]")
}

// LastEventID returns the Last-Event-ID from the request headers
// or the "last_event_id" query parameter. The value is truncated at
// the first CR/LF/NUL so a malicious resume token can't inject forged
// SSE fields when later echoed back to clients.
func LastEventID(r *http.Request) string {
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		return stripSSEControlChars(id)
	}
	return stripSSEControlChars(r.URL.Query().Get("last_event_id"))
}
