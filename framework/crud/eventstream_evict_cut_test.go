package crud

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// stalledWriter is a client that stopped reading: the subscribe comment goes
// through, then the first event frame parks in Write until the write
// deadline passes, the way a socket write to a full peer window parks.
// SSEWriter arms a 10-second deadline per write, so only a cut (a deadline
// already past) frees the write within the test's wait.
type stalledWriter struct {
	header    http.Header
	mu        sync.Mutex
	deadline  time.Time
	changed   chan struct{}
	stalled   chan struct{}
	stallOnce sync.Once
}

func newStalledWriter() *stalledWriter {
	return &stalledWriter{header: http.Header{}, changed: make(chan struct{}, 1), stalled: make(chan struct{})}
}

func (w *stalledWriter) Header() http.Header { return w.header }
func (w *stalledWriter) WriteHeader(int)     {}
func (w *stalledWriter) Flush()              {}

func (w *stalledWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, []byte("data:")) {
		return len(p), nil
	}
	w.stallOnce.Do(func() { close(w.stalled) })
	for {
		w.mu.Lock()
		d := w.deadline
		w.mu.Unlock()
		var expire <-chan time.Time
		if !d.IsZero() {
			wait := time.Until(d)
			if wait <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			expire = time.After(wait)
		}
		select {
		case <-expire:
		case <-w.changed:
		}
	}
}

func (w *stalledWriter) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	w.deadline = t
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return nil
}

// An evicted stream parked in a write to a client that stopped reading
// never reached its select, so it stayed resident after its seat went to
// the next stream, and the per-principal cap stopped bounding handlers
// (2026-10-27 D44). Eviction now cuts the parked write with a past write
// deadline, as the SSE broker does.
func TestEvictedStreamLeavesBlockedWrite(t *testing.T) {
	ch := seatCapFeedHandler(t,
		func(h *CrudHandler) { h.WithEventStreamSeats(1) },
		func(h *CrudHandler) { h.WithEventStreamSeatOverflow(stream.SeatOverflowEvictOldest) },
	)

	w := newStalledWriter()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/cap_feed", UserID: "alice"})
	ctxA, cancelA := context.WithCancel(req.Context())
	defer cancelA()
	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		ch.EventStream()(w, req.WithContext(ctxA))
	}()
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 1 })

	// Park A in a write: the event reaches its loop and the client never
	// drains it.
	deadline := time.Now().Add(3 * time.Second)
	for parked := false; !parked; {
		ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{
			"id": "n1", "user_id": "alice", "title": "t", "entity": "cap_feed", "ownerId": "alice",
		})
		select {
		case <-w.stalled:
			parked = true
		case <-time.After(20 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("the stream never wrote the event")
			}
		}
	}

	cancelB, doneB, _ := openSeatStream(t, ch, "alice")
	defer cancelB()
	select {
	case <-doneA:
	case <-time.After(3 * time.Second):
		t.Fatal("the evicted stream stayed parked in its write after its seat was taken")
	}
	select {
	case <-doneB:
		t.Fatal("the stream that took the seat ended")
	case <-time.After(50 * time.Millisecond):
	}
	if got := ch.seats.liveSeats(); got != 1 {
		t.Fatalf("live seats = %d, want 1", got)
	}
	cancelB()
	<-doneB
}

// Admission cuts only the seat it evicts: a stream seated below the cap, or
// a refused admission, leaves every live stream's write alone. A cut that
// lands after the handler returned would set a deadline on a response
// net/http may already have handed to the connection's next request, so a
// released seat ignores it.
func TestAdmitCutsOnlyTheEvictedSeat(t *testing.T) {
	cuts := map[string]int{}
	cutOf := func(name string) func() { return func() { cuts[name]++ } }
	want := func(step string, a, b, c int) {
		t.Helper()
		if cuts["a"] != a || cuts["b"] != b || cuts["c"] != c {
			t.Fatalf("%s: cuts = %v, want a=%d b=%d c=%d", step, cuts, a, b, c)
		}
	}
	r := newStreamSeatRegistry()
	evict := stream.SeatOverflowEvictOldest
	if _, ok := r.admit("p", 2, evict, cutOf("a")); !ok {
		t.Fatal("a refused")
	}
	b, _ := r.admit("p", 2, evict, cutOf("b"))
	want("below the cap", 0, 0, 0)
	if _, ok := r.admit("p", 2, stream.SeatOverflowRefuse, cutOf("x")); ok {
		t.Fatal("a refused admission was seated")
	}
	want("refused at the cap", 0, 0, 0)
	c, _ := r.admit("p", 2, evict, cutOf("c"))
	want("evicting the oldest", 1, 0, 0)

	r.release(b)
	b.interruptWrite()
	want("a released seat", 1, 0, 0)
	c.interruptWrite()
	want("a live seat", 1, 0, 1)
}
