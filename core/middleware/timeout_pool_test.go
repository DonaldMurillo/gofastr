package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"net"
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

// TestTimeoutLateStreamWriteRefused: a handler that flushed and then
// returned has handed the response back to net/http, so a write through
// the writer it kept must be refused, not passed to a finalized writer.
func TestTimeoutLateStreamWriteRefused(t *testing.T) {
	var kept http.ResponseWriter
	rec := httptest.NewRecorder()
	Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kept = w
		_, _ = w.Write([]byte("live"))
		w.(http.Flusher).Flush()
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if _, err := kept.Write([]byte("LEAK")); !errors.Is(err, errWriteAfterReturn) {
		t.Fatalf("late streamed write: err = %v, want errWriteAfterReturn", err)
	}
	if got := rec.Body.String(); got != "live" {
		t.Fatalf("body = %q, want %q", got, "live")
	}
}

// TestTimeoutLateFlushIsInert: a Flush after the handler returned must
// not re-send headers or flush a response net/http already finalized.
func TestTimeoutLateFlushIsInert(t *testing.T) {
	var kept http.ResponseWriter
	rec := httptest.NewRecorder()
	Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kept = w
		_, _ = w.Write([]byte("done"))
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	kept.WriteHeader(http.StatusTeapot)
	kept.(http.Flusher).Flush()
	if rec.Flushed || rec.Code != http.StatusOK {
		t.Fatalf("late Flush reached the writer: flushed=%v code=%d", rec.Flushed, rec.Code)
	}
	if _, err := kept.Write([]byte("LEAK")); !errors.Is(err, errWriteAfterReturn) {
		t.Fatalf("write after late Flush: err = %v, want errWriteAfterReturn", err)
	}
}

// TestTimeoutPanicReleasesWriter: a handler that panics has returned
// too, so its writer refuses writes from a goroutine it left behind.
func TestTimeoutPanicReleasesWriter(t *testing.T) {
	var kept http.ResponseWriter
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kept = w
		panic("boom")
	}))
	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if _, err := kept.Write([]byte("LEAK")); !errors.Is(err, errWriteAfterReturn) {
		t.Fatalf("write after panic: err = %v, want errWriteAfterReturn", err)
	}
}

// lateCallRecorder records every WriteHeader that reaches it and hijacks
// successfully, which httptest.ResponseRecorder does not.
type lateCallRecorder struct {
	*httptest.ResponseRecorder
	codes    []int
	hijacked bool
}

func (h *lateCallRecorder) WriteHeader(code int) {
	h.codes = append(h.codes, code)
	h.ResponseRecorder.WriteHeader(code)
}

func (h *lateCallRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// TestTimeoutLateStreamCallsInert: after a streamed handler returns, a
// late WriteHeader must not reach the writer and a late Hijack must not
// take the connection net/http is finishing.
func TestTimeoutLateStreamCallsInert(t *testing.T) {
	var kept http.ResponseWriter
	rec := &lateCallRecorder{ResponseRecorder: httptest.NewRecorder()}
	Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		kept = w
		w.(http.Flusher).Flush()
	})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	kept.WriteHeader(http.StatusTeapot)
	if len(rec.codes) != 0 {
		t.Errorf("late WriteHeader reached the writer: %v", rec.codes)
	}
	if _, _, err := kept.(http.Hijacker).Hijack(); !errors.Is(err, errWriteAfterReturn) || rec.hijacked {
		t.Errorf("late Hijack: err = %v, hijacked = %v", err, rec.hijacked)
	}
}
