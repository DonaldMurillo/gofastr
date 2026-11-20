package stream

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/sseframe"
)

// SSEWriter writes Server-Sent Events to an http.ResponseWriter, flushing
// each one and bounding each write with its own deadline.
type SSEWriter struct {
	w      http.ResponseWriter
	mu     sync.Mutex
	init   bool
	flush  http.Flusher
	nextID string
	rc     *http.ResponseController
}

var sseWriteTimeout = 10 * time.Second

// NewSSEWriter wraps w. It panics if w is not an http.Flusher.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flush, ok := w.(http.Flusher)
	if !ok {
		panic("stream: http.ResponseWriter does not implement http.Flusher")
	}
	return &SSEWriter{
		w:     w,
		flush: flush,
		rc:    http.NewResponseController(w),
	}
}

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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.armDeadline() == nil {
		s.flush.Flush()
		s.clearDeadline()
	}
}

func (s *SSEWriter) armDeadline() error {
	if err := s.rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func (s *SSEWriter) clearDeadline() {
	_ = s.rc.SetWriteDeadline(time.Time{})
}

func (s *SSEWriter) send(b string) error {
	if err := s.armDeadline(); err != nil {
		return err
	}
	_, err := s.w.Write([]byte(b))
	s.flush.Flush()
	s.clearDeadline()
	return err
}

// SetRetry writes the "retry:" field in milliseconds. A non-positive
// value writes nothing.
func (s *SSEWriter) SetRetry(seconds int) {
	if seconds <= 0 {
		return
	}
	retryMS := int64(seconds)
	if retryMS > math.MaxInt64/1000 {
		retryMS = math.MaxInt64
	} else {
		retryMS *= 1000
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()
	_ = s.send(fmt.Sprintf("retry: %d\n\n", retryMS))
}

// SetID queues an "id:" field to be emitted before the next event.
func (s *SSEWriter) SetID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID = id
}

// WriteEvent writes a named event, with the queued id, and flushes it.
// Names, ids and data are cleaned as Encode cleans them.
func (s *SSEWriter) WriteEvent(event, data string) error {
	return s.write(event, true, data)
}

// WriteData writes an unnamed event (type "message") and flushes it.
func (s *SSEWriter) WriteData(data string) error {
	return s.write("", false, data)
}

func (s *SSEWriter) write(event string, eventLine bool, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()
	var b strings.Builder
	sseframe.Append(&b, s.nextID, event, eventLine, data)
	s.nextID = ""
	return s.send(b.String())
}

// WriteComment writes a comment line, cut at its first CR, LF or NUL.
func (s *SSEWriter) WriteComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureHeaders()
	return s.send(": " + sseframe.Field(comment) + "\n\n")
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

// LastEventID returns Last-Event-ID or ?last_event_id, cut at CR, LF or NUL.
func LastEventID(r *http.Request) string {
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		return sseframe.Field(id)
	}
	return sseframe.Field(r.URL.Query().Get("last_event_id"))
}
