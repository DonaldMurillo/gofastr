//go:build red

package chat

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T1).
//
// Property: every long-lived stream surface enforces the house
// per-principal seat cap 16 — the one-number policy core/stream
// seats.go:3-9 (defaultSeatsPerPrincipal) states, pinned by siblings
// core/stream/persubcap_security_test.go (TestSSEPerPrincipalSubscriber
// Cap), core/mcp/sseats_security_test.go (TestSSESubscriberSeatsBounded)
// and framework/crud/eventstream_seats.go (EventStream re-authz per
// event + seats). One caller holds at most 16 resident streams; the
// 17th is refused at connect or displaces the oldest
// (seats.go SeatOverflowPolicy). Each seat is a goroutine plus a
// buffered channel, so an uncounted surface is a memory/fd exhaustion
// target for a single principal.
//
// Surfaces: kiln/live/sse.go::ServeSSE :69-111 — no authentication and
// no seat accounting; the handler loops on r.Context() for as long as
// the peer holds the connection. kiln/live/sse.go::Broadcaster.
// Subscribe :37-52 — unbounded subs map. Mount: kiln/chat/server.go:90
// wraps /.kiln/events in readGuard, a CSRF gate only — Origin-less
// peers (any local process, any scripted client) pass untouched.
// cmd/kiln/main.go:119-124 documents the posture: --addr 0.0.0.0:8765
// deliberately exposes the unauthenticated tool API to the network,
// and /.kiln/events rides the same listener.
//
// Finding (probe 2026-09-06/07): 64 concurrent anonymous GET
// /.kiln/events all admitted 200 text/event-stream; process goroutines
// went 7 -> 324. Nothing in the kiln live path counts, caps, or evicts
// subscribers.
//
// Fix direction: seat ServeSSE admission per principal (for this
// unauthenticated transport the principal is the TCP peer / the
// anonymous bucket) with the seats.go default 16, answering 429 at
// connect or evicting the oldest, and free the seat when the stream
// context ends.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestKilnEventsRedSeatCapped: 17 concurrent anonymous GET /.kiln/events
// (no Origin header — the caller class readGuard is documented to pass).
// At most 16 may be admitted; the 17th must be refused at connect or
// displace the oldest. A cross-site control leg proves the requests run
// through the mounted readGuard, not around it.
func TestKilnEventsRedSeatCapped(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-events-seats-red")
	if err != nil {
		t.Fatal("setup broken: EphemeralSQLite:", err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal("setup broken: live.New:", err)
	}
	tools := protocol.New(l)
	srv := New(l, tools)
	srv.Mount(l.Aux())
	ts := httptest.NewServer(l)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const seats = 16 // core/stream seats.go defaultSeatsPerPrincipal
	const dials = seats + 1

	type seatCapResult struct {
		admitted bool
		status   int
	}
	results := make(chan seatCapResult, dials)
	done := make(chan struct{}, dials)
	for range dials {
		go func() {
			defer func() { done <- struct{}{} }()
			req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/.kiln/events", nil)
			if rerr != nil {
				results <- seatCapResult{}
				return
			}
			resp, rerr := ts.Client().Do(req)
			if rerr != nil {
				results <- seatCapResult{}
				return
			}
			admitted := resp.StatusCode == http.StatusOK &&
				strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
			results <- seatCapResult{admitted: admitted, status: resp.StatusCode}
			if !admitted {
				resp.Body.Close()
				return
			}
			// Keep the stream open the way a real subscriber does; the
			// read parks until the server closes it or ctx is cancelled.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}

	admitted, resolved := 0, 0
	verdict := time.After(5 * time.Second)
	for resolved < dials {
		select {
		case r := <-results:
			resolved++
			if r.admitted {
				admitted++
			}
		case <-verdict:
			t.Fatalf("setup broken: only %d of %d streams reached a connect verdict within 5s", resolved, dials)
		}
	}

	if admitted > seats {
		t.Errorf("SECURITY: [kiln-events-seats] %d concurrent anonymous streams on GET /.kiln/events were admitted (bound %d): ServeSSE does no per-principal seat accounting, so one Origin-less caller holds a goroutine plus a buffered channel per dial for as long as it cares to hold them (probe: 64/64 admitted, goroutines 7 -> 324)", admitted, seats)
	}
	if dials-admitted < 1 {
		t.Errorf("SECURITY: [kiln-events-seats] every one of %d anonymous dials was admitted — the (seats+1)th stream must be refused at connect (429) or displace the oldest, the seats.go SeatOverflowPolicy contract every other stream surface in the tree already implements", dials)
	}

	// Control leg: the CSRF gate on the same surface still refuses a
	// cross-site browser peer, so the admissions above ran through the
	// mounted readGuard, not around it.
	ctrlReq, cerr := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/.kiln/events", nil)
	if cerr != nil {
		t.Fatal("setup broken: control request:", cerr)
	}
	ctrlReq.Header.Set("Sec-Fetch-Site", "cross-site")
	ctrlReq.Header.Set("Origin", "https://evil.example.com")
	cresp, cerr := ts.Client().Do(ctrlReq)
	if cerr != nil {
		t.Fatal("setup broken: cross-site control request:", cerr)
	}
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup broken: cross-site GET /.kiln/events = %d, want 403 (readGuard leg)", cresp.StatusCode)
	}

	// Cancel the shared context and bound the join: every held stream
	// must unwind when its request context dies.
	cancel()
	join := time.After(5 * time.Second)
	for range dials {
		select {
		case <-done:
		case <-join:
			return // bounded; the t.Cleanup(cancel) re-covers stragglers
		}
	}
}
