//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2, example-grade). Tests-only; no fix
// applied.
//
// Property: authenticated long-lived stream surfaces enforce the
// per-principal seat cap — the one-number policy core/stream/seats.go:3-9
// (defaultSeatsPerPrincipal=16) states, pinned by siblings
// core/stream/persubcap_security_test.go, core/mcp/sseats_security_test.go,
// framework/crud/eventstream_seats.go, and this round's harness
// ws/rest seat caps. The support-assist websocket is the unseated
// member: the support cookie is ONE principal (valid against every
// session), so per-role sockets are per-credential seats — at most 16
// may be resident; the 17th is refused at upgrade or displaces the
// oldest (seats.go SeatOverflowPolicy).
//
// Surfaces: session.go::handleWS :686-756 — support-cookie gate
// :690-694, upgrade :710, then s.conns[r] = conn at :725-730
// OVERWRITES the previous socket for the role without closing it, and
// the handler parks in the blocking conn.Read loop :739-754. Every
// accepted upgrade registers a *WebSocketConn in the StateChannel's
// conns map (core/stream state_channel.go:102) plus a write/read pump
// and keepalive per conn; nothing bounds them per credential, and the
// reconnect path can never free an older seat (the displaced socket
// keeps its channel registration until its TCP connection dies on its
// own).
//
// Finding (probe 2026-09-06/07): 17/17 same-cookie sequential upgrades
// against /support/session/{id}/ws?client=c<i>; StateChannel Count()
// reached 17 and the first socket was still live after the 17th
// landed.
//
// Fix direction: seat the upgrade per role credential at handleWS
// admission — 429 at upgrade or evict-oldest closing the displaced
// socket (which also fixes :729: closing the conn it replaces frees
// the channel seat and its s.conns entry via Unregister/OnClose).

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seatRedDial hand-rolls one RFC 6455 upgrade against srv with the
// support cookie (the redDialWS handshake grammar, seat-red prefixed),
// leaving the socket open. A nil reader marks a refused upgrade; the
// conn is still returned so the caller's cleanup closes it.
func seatRedDial(t *testing.T, srv *httptest.Server, path, cookieHeader string) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("setup broken: dial %s: %v", addr, err)
	}
	var keyBytes [16]byte // base64 -> the canonical 24-char key Upgrade validates
	if _, err := rand.Read(keyBytes[:]); err != nil {
		conn.Close()
		t.Fatalf("setup broken: ws key: %v", err)
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Origin: http://" + addr + "\r\n" +
		"Cookie: " + cookieHeader + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(keyBytes[:]) + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("setup broken: write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, " 101 ") {
		conn.SetReadDeadline(time.Time{})
		return conn, nil // refused or closed at connect
	}
	for {
		line, rerr := br.ReadString('\n')
		if rerr != nil {
			conn.Close()
			t.Fatalf("setup broken: handshake headers: %v", rerr)
		}
		if line == "\r\n" {
			break
		}
	}
	conn.SetReadDeadline(time.Time{})
	return conn, br
}

// TestWSSeatRedCappedPerRole: 17 sequential upgrades with ONE support
// cookie (the shared principal) against ONE session's support socket,
// each socket held open. At most 16 may still be live once the 17th
// lands — the 17th refused at upgrade, or the oldest displaced and
// closed — and the session's StateChannel must register at most 16
// connections.
func TestWSSeatRedCappedPerRole(t *testing.T) {
	srv, a := newTestApp(t)
	cookie, id := newSupportSession(t, srv, a)
	cookieHeader := cookie.Name + "=" + cookie.Value

	const seats = 16 // core/stream seats.go defaultSeatsPerPrincipal
	const dials = seats + 1

	conns := make([]net.Conn, 0, dials)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	readers := make([]*bufio.Reader, dials)
	upgraded := make([]bool, dials)
	for i := range dials {
		path := fmt.Sprintf("/support/session/%s/ws?client=c%d", id, i)
		conn, br := seatRedDial(t, srv, path, cookieHeader)
		conns = append(conns, conn)
		readers[i] = br
		upgraded[i] = br != nil
	}
	if !upgraded[0] {
		t.Fatal("setup broken: the first support-role socket did not upgrade — the surface must admit the first seat for this test to say anything about the 17th")
	}

	// The channel registers each conn through its async Connect jobs;
	// bounded wait for the count to settle at what the dials earned.
	count := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := a.lookup(id); s != nil {
			count = s.channel.Count()
		}
		if count >= dials {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Give an evict-oldest policy a moment to displace the oldest
	// seat before counting (a refusal needs none: it answered above).
	time.Sleep(250 * time.Millisecond)

	// A seat freed by eviction is observable as the server closing the
	// socket: bounded liveness probe per upgraded conn (snapshot,
	// presence or keepalive frames flowing, or silent-but-open, all
	// count as held).
	seatAlive := func(i int) bool {
		conns[i].SetReadDeadline(time.Now().Add(200 * time.Millisecond))
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
	upgradedCount := 0
	for i := range conns {
		if upgraded[i] {
			upgradedCount++
			if seatAlive(i) {
				live++
			}
		}
	}
	if live > seats || count > seats {
		t.Errorf("SECURITY: [webmcp-ws-seats] one support-role credential holds %d live sockets on one session's channel (upgraded %d/%d, StateChannel Count()=%d, bound %d): handleWS authorizes the support cookie and upgrades without seating, so every extra dial parks a handler in conn.Read plus a channel registration and its pumps, and the reconnect path overwrites s.conns[r] without closing the displaced socket — seats are never freed while the TCP connections live (probe: 17/17 upgraded, first socket still live after the 17th landed)",
			live, upgradedCount, dials, count, seats)
	}
}
