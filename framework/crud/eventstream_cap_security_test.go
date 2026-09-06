package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// Property: one authenticated principal cannot hold an unbounded number of
// concurrent SSE event streams on a crud entity's EventStream surface.
//
// Found red in the 2026-09-05 adversarial pass round 4 (F26): the connect
// path in crud_events.go ran its auth/tenant/permission gates but had no
// admission cap, so one user (or a scripted client) held every stream it
// opened — each pinning a goroutine, a 32-entry buffer, and three bus
// subscriptions until disconnect — bounded only by FDs and memory.
//
// Fixed by the per-principal seat cap in eventstream_seats.go, decided
// (2026-09-06) to mirror core/stream's SSE broker and core/mcp's SSE stream:
// EventStreamSeats (0 = the 16 default, negative lifts it) and
// EventStreamSeatOverflow ({SeatOverflowRefuse, 429 + Retry-After at connect,
// the default | SeatOverflowEvictOldest}). The zero-value config gets the
// default cap; seats are accounted per principal on connect and released on
// disconnect, write error, re-auth refusal, or eviction.

// seatCapFeedHandler builds the feed the tests open streams against: one
// owner-scoped entity with a live event bus.
func seatCapFeedHandler(t *testing.T, mutators ...func(*CrudHandler)) *CrudHandler {
	t.Helper()
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("cap_feed", "cap_feed", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE cap_feed (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`)
	ch.Events = event.NewEventBus()
	for _, m := range mutators {
		m(ch)
	}
	return ch
}

type seatStreamResult struct {
	code       int
	retryAfter string
}

// openSeatStream launches one EventStream connect for user on its own
// context + recorder. When the handler RETURNS it reports the response code
// and Retry-After on results — only a refused (or closed) stream returns
// before cancel, so the pre-cancel traffic on results is exactly the
// refusals, race-free (each recorder stays owned by its own goroutine).
func openSeatStream(t *testing.T, ch *CrudHandler, user string) (cancel context.CancelFunc, done chan struct{}, results chan seatStreamResult) {
	t.Helper()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/cap_feed", UserID: user})
	sctx, cancel := context.WithCancel(req.Context())
	rec := httptest.NewRecorder()
	done = make(chan struct{})
	results = make(chan seatStreamResult, 1)
	go func() {
		defer close(done)
		ch.EventStream()(rec, req.WithContext(sctx))
		results <- seatStreamResult{code: rec.Code, retryAfter: rec.Header().Get("Retry-After")}
	}()
	return cancel, done, results
}

// seatWaitUntil polls cond until it holds or the deadline passes.
func seatWaitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not hold within 3s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestEventStreamPerUserIsBounded opens defaultEventStreamSeats+8 concurrent
// streams for ONE user under the zero-value config and asserts the bound:
// exactly the cap held, the rest refused 429 with Retry-After at connect,
// another principal unaffected, and every seat released on disconnect.
func TestEventStreamPerUserIsBounded(t *testing.T) {
	ch := seatCapFeedHandler(t)

	const streams = defaultEventStreamSeats + 8
	cancels := make([]context.CancelFunc, 0, streams)
	dones := make([]chan struct{}, 0, streams)
	results := make([]chan seatStreamResult, 0, streams)
	for range streams {
		cancel, done, res := openSeatStream(t, ch, "alice")
		cancels = append(cancels, cancel)
		dones = append(dones, done)
		results = append(results, res)
	}
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == defaultEventStreamSeats })

	// Emitting into the admitted set must not panic or wedge: the live
	// streams' subscriptions are the fan-out the cap bounds.
	ch.EmitEvent(context.Background(), event.EntityCreated, map[string]any{
		"id": "n1", "title": "t", "entity": "cap_feed", "ownerId": "alice",
	})

	// Fan the per-stream results into one buffered channel: every stream
	// writes exactly one result (refused ones now, live ones after the
	// cancels below), so nothing ever blocks on the merge.
	merged := make(chan seatStreamResult, streams)
	for _, res := range results {
		go func(c chan seatStreamResult) { merged <- <-c }(res)
	}

	// The refused remainder is answered at connect (429 + Retry-After),
	// not left holding an empty stream.
	wantRefused := streams - defaultEventStreamSeats
	var refused int
	deadline := time.After(3 * time.Second)
	for refused < wantRefused {
		select {
		case res := <-merged:
			refused++
			if res.code != http.StatusTooManyRequests {
				t.Errorf("seat-refused stream answered %d, want 429", res.code)
			}
			if res.retryAfter == "" {
				t.Error("seat-refused 429 carries no Retry-After header for EventSource backoff")
			}
		case <-deadline:
			t.Fatalf("only %d of %d streams were refused at connect", refused, wantRefused)
		}
	}

	// Per-principal accounting: alice is at her cap, bob is a different
	// principal and must still be admitted.
	bobCancel, bobDone, bobRes := openSeatStream(t, ch, "bob")
	select {
	case <-bobDone:
		t.Error("a second principal was refused under the first principal's seat cap")
	case <-time.After(100 * time.Millisecond):
	}
	bobCancel()
	<-bobDone
	if res := <-bobRes; res.code != http.StatusOK {
		t.Errorf("bob's stream ended with %d before cancel", res.code)
	}

	// Disconnect accounting: every seat is released on cancel.
	for _, c := range cancels {
		c()
	}
	var wg sync.WaitGroup
	for _, d := range dones {
		wg.Add(1)
		go func(d chan struct{}) { <-d; wg.Done() }(d)
	}
	wg.Wait()
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 0 })
}

// TestEventStreamSeatCapEvictOldest pins the configurable overflow policy:
// past the cap the principal's OLDEST stream is closed and the new one
// seated; the count never exceeds the cap, and the closed stream's handler
// returns (its bus subscriptions with it).
func TestEventStreamSeatCapEvictOldest(t *testing.T) {
	ch := seatCapFeedHandler(t,
		func(h *CrudHandler) { h.WithEventStreamSeats(2) },
		func(h *CrudHandler) { h.WithEventStreamSeatOverflow(stream.SeatOverflowEvictOldest) },
	)

	cancelA, doneA, _ := openSeatStream(t, ch, "alice")
	defer cancelA()
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 1 })

	cancelB, doneB, resB := openSeatStream(t, ch, "alice")
	defer cancelB()
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 2 })

	// The third connect evicts the OLDEST seat (A) and seats C.
	cancelC, doneC, _ := openSeatStream(t, ch, "alice")
	defer cancelC()
	select {
	case <-doneA:
		// A's handler returned: the eviction closed it.
	case <-time.After(3 * time.Second):
		t.Fatal("EvictOldest did not close the principal's oldest stream")
	}
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 2 })
	if got := ch.seats.liveSeats(); got > 2 {
		t.Errorf("principal holds %d seats, cap is 2", got)
	}

	// B survives the eviction of A; both B and C depart on cancel and the
	// registry drains to zero.
	cancelB()
	<-doneB
	if res := <-resB; res.code != http.StatusOK {
		t.Errorf("eviction targeted the oldest stream, but B ended with %d", res.code)
	}
	cancelC()
	<-doneC
	seatWaitUntil(t, func() bool { return ch.seats.liveSeats() == 0 })
}
