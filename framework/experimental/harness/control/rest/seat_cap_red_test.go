//go:build red

package rest

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only; no fix applied.
//
// Property: authenticated long-lived stream surfaces enforce the
// per-principal seat cap — the one-number policy core/stream/seats.go:3-9
// (defaultSeatsPerPrincipal=16) states, pinned by siblings
// core/stream/persubcap_security_test.go, core/mcp/sseats_security_test.go,
// framework/crud/eventstream_seats.go, and this round's
// ws/seat_cap_red_test.go for the harness control websocket. The REST
// SSE arm of the same control tree is the unseated member: one
// credential may hold at most 16 resident streams; the 17th is refused
// at connect or displaces the oldest (seats.go SeatOverflowPolicy).
//
// The credential half of the contract is already pinned on this exact
// handler: rest_security_test.go TestRestSSEStreamStopsOnRevocation
// (token re-verified per event plus the 10s ticker). The seat cap is
// the unpinned half.
//
// Surfaces: rest.go::handleSSE :348-392 — headers written and flushed
// at :353-357 before the bus subscribe at :360, then the handler parks
// in its select loop for the life of the request; no seat accounting.
// Each admitted stream costs a handler goroutine, an eng.Bus
// subscription and a revocation ticker; a 17th GET is as cheap as the
// 1st.
//
// Finding (probe 2026-09-06/07): 33/33 same-token concurrent
// GET /v1/sessions/{id}/events admitted 200 text/event-stream against
// one session; nothing refused, nothing evicted.
//
// Fix direction: seat the stream per token principal (claims JTI /
// identity) at admission — 429 at connect or evict-oldest, the
// core/stream SeatOverflowPolicy shape — and free the seat when the
// select loop exits.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSSEStreamsRedCappedPerToken: 17 sequential raw-HTTP GETs of
// /v1/sessions/{id}/events (the ws twin's grammar; sequential so the
// 17th is unambiguously last) with ONE token against ONE session, each
// stream held open past its flushed headers. At most 16 may still be
// live once the 17th lands — the 17th refused at connect, or the
// oldest displaced and closed.
func TestSSEStreamsRedCappedPerToken(t *testing.T) {
	s, sess, _, tok := newWiredTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	const seats = 16 // core/stream seats.go defaultSeatsPerPrincipal
	const dials = seats + 1

	conns := make([]net.Conn, 0, dials)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	readers := make([]*bufio.Reader, dials)
	admitted := make([]bool, dials)
	for i := range dials {
		conn, err := net.Dial("tcp", host)
		if err != nil {
			t.Fatalf("setup broken: dial %d: %v", i, err)
		}
		conns = append(conns, conn)
		fmt.Fprintf(conn, "GET /v1/sessions/%s/events HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"X-Harness-Token: %s\r\n"+
			"\r\n", sess, host, tok)
		// Read the response head (through the blank line) with a
		// bounded deadline; the handler flushes headers immediately,
		// so a refused stream answers non-200 here, an admitted one
		// parks open past them.
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(conn)
		var head bytes.Buffer
		for !bytes.Contains(head.Bytes(), []byte("\r\n\r\n")) {
			chunk := make([]byte, 1024)
			n, rerr := br.Read(chunk)
			head.Write(chunk[:n])
			if rerr != nil {
				break
			}
		}
		h := head.String()
		readers[i] = br
		admitted[i] = strings.HasPrefix(h, "HTTP/1.1 200") &&
			strings.Contains(h, "text/event-stream")
	}
	if !admitted[0] {
		t.Fatal("setup broken: the first same-token stream was not admitted 200 text/event-stream — the surface must seat the first stream for this test to say anything about the 17th")
	}

	// Give an evict-oldest policy a moment to displace the oldest
	// seat before counting (a refusal needs none: it answered above).
	time.Sleep(250 * time.Millisecond)

	// A seat freed by eviction is observable as the server closing the
	// stream: bounded liveness probe per admitted conn (events flowing
	// or silent-but-open both count as held).
	seatCapAlive := func(i int) bool {
		conn := conns[i]
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		buf := make([]byte, 512)
		n, err := readers[i].Read(buf)
		if n > 0 || err == nil {
			return true
		}
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() {
			return true
		}
		return false // EOF / reset: the server closed it
	}
	live := 0
	admittedCount := 0
	for i := range conns {
		if admitted[i] {
			admittedCount++
			if seatCapAlive(i) {
				live++
			}
		}
	}
	if live > seats {
		t.Errorf("SECURITY: [harness-rest-seats] one token holds %d concurrent SSE event streams against one session (admitted %d/%d, bound %d): handleSSE re-verifies the credential per event but never seats it, so every extra GET /v1/sessions/{id}/events parks a handler goroutine, a bus subscription and a revocation ticker for the life of the request (probe: 33/33 same-token streams admitted)", live, admittedCount, dials, seats)
	}
}
