//go:build red

package ws

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
//
// Property: authenticated long-lived stream surfaces enforce the
// per-principal seat cap — the one-number policy core/stream seats.go:3-9
// (defaultSeatsPerPrincipal=16) states, pinned by siblings
// core/stream/persubcap_security_test.go (TestSSEPerPrincipalSubscriber
// Cap), core/mcp/sseats_security_test.go (TestSSESubscriberSeatsBounded)
// and framework/crud/eventstream_seats.go. One credential may hold at
// most 16 resident streams; the 17th is refused at connect or
// displaces the oldest (seats.go SeatOverflowPolicy).
//
// The credential half of the contract is already pinned on this exact
// surface family: the harness re-verifies the token per event plus a
// 10s ticker (rest twin pinned by rest_security_test.go
// TestRestSSEStreamStopsOnRevocation; this handler implements the same
// revocationWatch). The seat cap is the unpinned half.
//
// Surfaces: ws.go::Handler.ServeHTTP :65-145 — token verify + session
// bind + upgrade, no seat accounting; each accepted socket costs its
// run/eventPump/revocationWatch goroutines for the life of the TCP
// connection. A 4th dial by the same token is as cheap as the 1st.
//
// Finding (probe 2026-09-06/07): 33/33 same-token sockets upgraded
// (101) against one session; nothing refused, nothing evicted.
//
// Fix direction: seat the upgrade per token principal (claims JTI /
// identity) before the Hijack, answering 429 at connect or evicting
// the oldest, and free the seat when the socket's run loop exits.

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// TestWSSocketsRedCappedPerToken: 17 sequential hand-rolled handshakes
// (the ws_test.go grammar) with ONE token against ONE session. At most
// 16 sockets may still be live once the 17th lands — the 17th refused
// at connect, or the oldest displaced and closed.
func TestWSSocketsRedCappedPerToken(t *testing.T) {
	urlStr, session, tok, cleanup := setupServer(t)
	defer cleanup()
	host := strings.TrimPrefix(urlStr, "http://")

	const seats = 16 // core/stream seats.go defaultSeatsPerPrincipal
	const dials = seats + 1

	conns := make([]net.Conn, 0, dials)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	upgraded := make([]bool, dials)
	for i := range dials {
		conn, err := net.Dial("tcp", host)
		if err != nil {
			t.Fatalf("setup broken: dial %d: %v", i, err)
		}
		conns = append(conns, conn)
		fmt.Fprintf(conn, "GET /?session=%s&token=%s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
			"Sec-WebSocket-Version: 13\r\n\r\n",
			session, tok, host)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 4096)
		n, rerr := conn.Read(buf)
		if rerr != nil && n == 0 {
			upgraded[i] = false // refused or closed at connect
			continue
		}
		line := string(buf[:n])
		if idx := bytes.IndexByte(buf[:n], '\n'); idx >= 0 {
			line = string(buf[:idx])
		}
		upgraded[i] = strings.HasPrefix(line, "HTTP/1.1 101")
	}
	if !upgraded[0] {
		t.Fatal("setup broken: the first same-token socket did not upgrade — the surface must admit the first seat for this test to say anything about the 17th")
	}

	// A seat freed by eviction is observable as the server closing the
	// socket: bounded liveness probe per upgraded conn (frames flowing
	// or silent-but-open both count as held).
	seatCapAlive := func(i int) bool {
		conn := conns[i]
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
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
	for i := range conns {
		if upgraded[i] && seatCapAlive(i) {
			live++
		}
	}
	if live > seats {
		t.Errorf("SECURITY: [harness-ws-seats] one token holds %d concurrent upgraded control sockets against one session (bound %d): Handler.ServeHTTP verifies the credential and binds the session but never seats it, so every extra dial parks a run loop, an event pump and a revocation watcher for the life of the TCP connection (probe: 33/33 same-token upgrades)", live, seats)
	}
}
