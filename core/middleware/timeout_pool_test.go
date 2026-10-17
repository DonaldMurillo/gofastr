package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeoutLateWriteIsRefused: Timeout recycles its body buffers, so a
// write through a writer whose handler already returned must not reach
// the buffer. By then the pool may have handed it to the next request,
// and the late bytes would ship inside someone else's response.
func TestTimeoutLateWriteIsRefused(t *testing.T) {
	var kept http.ResponseWriter
	mw := Timeout(time.Second)
	first := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kept = w
		_, _ = w.Write([]byte("first"))
	}))
	first.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	if _, err := kept.Write([]byte("LEAK")); !errors.Is(err, errWriteAfterReturn) {
		t.Fatalf("late write: err = %v, want errWriteAfterReturn", err)
	}

	second := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("second"))
	}))
	rec := httptest.NewRecorder()
	second.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Body.String(); got != "second" {
		t.Fatalf("second response = %q, want %q", got, "second")
	}
}

// TestTimeoutRecycledBufferStartsEmpty: a timed-out response abandons
// its buffer undrained, and the pool must not hand those bytes to the
// next request. A committed response drains its buffer on the way out, so
// the abandoned one is the case that needs the reset.
func TestTimeoutRecycledBufferStartsEmpty(t *testing.T) {
	stale := newTimeoutWriter(httptest.NewRecorder())
	_, _ = stale.Write([]byte("stale"))
	stale.expire()
	stale.release()

	rec := httptest.NewRecorder()
	fresh := newTimeoutWriter(rec)
	_, _ = fresh.Write([]byte("b"))
	fresh.finish()
	if got := rec.Body.String(); got != "b" {
		t.Fatalf("response = %q, want %q", got, "b")
	}
}

// TestTimeoutBigBufferNotPooled: a buffer past maxPooledBody is dropped,
// so one multi-megabyte response doesn't stay pinned in the pool.
func TestTimeoutBigBufferNotPooled(t *testing.T) {
	if !poolableBody(bytes.NewBuffer(make([]byte, 0, maxPooledBody))) {
		t.Error("a buffer at the cap was refused")
	}
	if poolableBody(bytes.NewBuffer(make([]byte, 0, maxPooledBody+1))) {
		t.Error("a buffer past the cap was pooled")
	}
}
