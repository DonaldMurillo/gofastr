package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// streamBase is a recorder with the net/http writer's deadline and
// flush surface, so http.ResponseController has something to reach.
type streamBase struct {
	*httptest.ResponseRecorder
	writeDeadlineSet bool
}

func (f *streamBase) SetWriteDeadline(time.Time) error { f.writeDeadlineSet = true; return nil }

// streamThrough serves one request through mw to a handler that sets a
// write deadline and flushes, and reports what reached the client.
func streamThrough(t *testing.T, mw Middleware, req func() *http.Request, calls *atomic.Int32) {
	t.Helper()
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			t.Errorf("SetWriteDeadline through the wrapper: %v", err)
		}
		_, _ = w.Write([]byte("data: one\n\n"))
		if err := rc.Flush(); err != nil {
			t.Errorf("Flush through the wrapper: %v", err)
		}
	}))
	base := &streamBase{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(base, req())
	if !base.writeDeadlineSet {
		t.Fatal("the write deadline never reached the underlying writer")
	}
	if !base.Flushed || base.Body.String() != "data: one\n\n" {
		t.Fatalf("flushed %v, body %q: the event never reached the client", base.Flushed, base.Body.String())
	}
}

// The idempotency recorder hid the connection from
// http.ResponseController, so a streaming handler behind it had no write
// deadline (2026-10-27 P40). A streamed response is not kept for replay.
func TestIdempotencyLetsStreamsThrough(t *testing.T) {
	var calls atomic.Int32
	mw := Idempotency(IdempotencyConfig{Principal: testPrincipal})
	post := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(IdempotencyKeyHeader, "stream-key")
		return req
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("data: one\n\n"))
		http.NewResponseController(w).Flush()
	}))
	streamThrough(t, mw, post, new(atomic.Int32))
	for range 2 {
		h.ServeHTTP(httptest.NewRecorder(), post())
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler ran %d times for two streamed POSTs, want 2: a streamed response was replayed", got)
	}
}

// The timeout writer hid the connection the same way (2026-10-27 P1).
func TestTimeoutLetsStreamsThrough(t *testing.T) {
	get := func() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) }
	streamThrough(t, Timeout(5*time.Second), get, new(atomic.Int32))
}
