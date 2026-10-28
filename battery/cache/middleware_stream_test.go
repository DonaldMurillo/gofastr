package cache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// streamWriter is a recorder with the net/http writer's deadline and
// flush surface, so http.ResponseController has something to reach.
type streamWriter struct {
	*httptest.ResponseRecorder
	deadlineSet bool
}

func (s *streamWriter) SetWriteDeadline(time.Time) error { s.deadlineSet = true; return nil }

// The cache recorder hid the client writer from http.ResponseController:
// an SSE handler behind the cache could not flush or set a write
// deadline, so its events sat in the buffer (2026-10-27 B16). A flush
// streams the response through, uncached.
func TestCacheLetsStreamsFlush(t *testing.T) {
	h, runs := countingCache(nil, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			t.Errorf("SetWriteDeadline behind the cache: %v", err)
		}
		_, _ = w.Write([]byte("data: one\n\n"))
		if err := rc.Flush(); err != nil {
			t.Errorf("Flush behind the cache: %v", err)
		}
	})
	for range 2 {
		sw := &streamWriter{ResponseRecorder: httptest.NewRecorder()}
		h.ServeHTTP(sw, plainGet())
		if !sw.Flushed || sw.Body.String() != "data: one\n\n" {
			t.Fatalf("flushed %v, body %q: the event never reached the client", sw.Flushed, sw.Body.String())
		}
		if ct := sw.Header().Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want the handler's header", ct)
		}
		if !sw.deadlineSet {
			t.Fatal("the write deadline never reached the client writer")
		}
	}
	if *runs != 2 {
		t.Fatalf("handler ran %d times, want 2: a streamed response was cached", *runs)
	}
}
