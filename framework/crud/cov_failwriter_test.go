package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// covSSEFailWriter is a Flusher-capable ResponseWriter whose Write fails once
// calls exceed failAfter — modelling an SSE subscriber that disconnects.
type covSSEFailWriter struct {
	hdr       http.Header
	calls     int
	failAfter int
}

func (w *covSSEFailWriter) Header() http.Header {
	if w.hdr == nil {
		w.hdr = http.Header{}
	}
	return w.hdr
}
func (w *covSSEFailWriter) WriteHeader(int) {}
func (w *covSSEFailWriter) Write(b []byte) (int, error) {
	w.calls++
	if w.calls > w.failAfter {
		return 0, errors.New("client disconnected")
	}
	return len(b), nil
}
func (w *covSSEFailWriter) Flush() {}

// Stream row-encode failure (crud_stream.go:106): the prefix write succeeds,
// then the first row's json.Encode write fails — modelling a client that
// disconnects mid-stream. failAfter=1 lets the `{"data":[` prefix through and
// fails the very next write (the first row's Encode).
func TestStream_EncodeErrorAborts(t *testing.T) {
	ch, _ := covItems(t, nil, 5)
	w := &covFailWriter{failAfter: 1}
	req := withTestUser(httptest.NewRequest("GET", "/items?stream=true", nil), "u1")
	ch.List()(w, req)
	// Handler must return cleanly (no panic) after the encode write fails.
	if w.calls < 2 {
		t.Fatalf("expected at least prefix + one row-encode write, got %d", w.calls)
	}
}

// SSE WriteEvent failure (crud_events.go:153): a matching event reaches the
// subscriber but the write fails (client gone), so the stream loop returns.
func TestEventStream_WriteErrorReturns(t *testing.T) {
	ch, _ := covOwnerNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	ctx, cancel := context.WithCancel(ctxWithUser("alice"))
	defer cancel()
	w := &covSSEFailWriter{failAfter: 0} // every write fails
	req := httptest.NewRequest("GET", "/onotes/_events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { ch.EventStream()(w, req); close(done) }()

	time.Sleep(50 * time.Millisecond)
	ch.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "n1", "user_id": "alice"})

	select {
	case <-done: // WriteEvent error path returned the handler
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("EventStream did not return after SSE write error")
	}
}

// SSE marshal failure (crud_events.go:150): an event whose data can't be
// JSON-encoded is skipped (continue), not fatal — the stream keeps running
// until the client disconnects.
func TestEventStream_MarshalErrorContinues(t *testing.T) {
	ch, _ := covOwnerNotesHandler(t)
	bus := event.NewEventBus()
	ch.Events = bus

	ctx, cancel := context.WithCancel(ctxWithUser("alice"))
	rec := httptest.NewRecorder() // succeeds + implements Flusher
	req := httptest.NewRequest("GET", "/onotes/_events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req); close(done) }()

	time.Sleep(50 * time.Millisecond)
	// A channel in the data makes json.Marshal(event) fail → continue.
	ch.EmitEvent(ctxWithUser("alice"), event.EntityCreated, map[string]any{"id": "n1", "user_id": "alice", "bad": make(chan int)})
	time.Sleep(80 * time.Millisecond)
	cancel() // ends the loop via ctx.Done()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("EventStream did not return after ctx cancel")
	}
}

var _ = http.StatusOK
