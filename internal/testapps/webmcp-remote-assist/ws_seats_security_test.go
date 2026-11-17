package main

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

// Pins: the per-role websocket upgrade is seated against the role
// credential — at most wsSeatsPerCredential (16, core/stream's
// defaultSeatsPerPrincipal number) concurrent sockets per credential,
// the 17th refused 429 at the upgrade before any pump or channel
// registration exists. Pinned siblings: core/stream/persubcap_security_test.go,
// core/mcp/sseats_security_test.go, framework/crud/eventstream_seats.go,
// and this round's harness ws/rest seat caps.
//
// Surfaces: session.go::handleWS — reserveWSSeat BEFORE the upgrade
// handshake, unseatWSSeat on handler return; the role reconnect path
// also closes the transport it supplants in s.conns. Before the fix
// every accepted upgrade parked a handler in conn.Read plus a
// StateChannel registration and its pumps, unbounded per credential,
// and the reconnect path overwrote s.conns[r] without closing the
// displaced socket (probe: 17/17 same-cookie upgrades, first socket
// still live after the 17th landed).

// seatDial hand-rolls one RFC 6455 upgrade against srv with the
// support cookie, leaving the socket open. A nil reader marks a refused
// upgrade; the conn is still returned so the caller's cleanup closes it.
func seatDial(t *testing.T, srv *httptest.Server, path, cookieHeader string) (net.Conn, *bufio.Reader) {
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

// TestWSSeatCappedPerRole: 17 sequential upgrades with ONE support
// cookie (the shared principal) against ONE session's support socket,
// each socket held open. At most 16 may still be live once the 17th
// lands — the 17th refused at upgrade — and the session's StateChannel
// must register at most 16 connections.
func TestWSSeatCappedPerRole(t *testing.T) {
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
		conn, br := seatDial(t, srv, path, cookieHeader)
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
	time.Sleep(250 * time.Millisecond)

	// A held seat is observable as the server keeping the socket open:
	// bounded liveness probe per upgraded conn (snapshot, presence or
	// keepalive frames flowing, or silent-but-open, all count as held).
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
		t.Errorf("SECURITY: [webmcp-ws-seats] one support-role credential holds %d live sockets on one session's channel (upgraded %d/%d, StateChannel Count()=%d, bound %d): an unseated upgrade path parks a handler in conn.Read plus a channel registration and its pumps for every dial, and the reconnect path overwrites s.conns[r] without closing the displaced socket — seats are never freed while the TCP connections live",
			live, upgradedCount, dials, count, seats)
	}
	if upgradedCount == dials {
		t.Errorf("SECURITY: [webmcp-ws-seats] all %d same-credential upgrades were admitted — the seat cap must refuse the %dth at the upgrade (429), not seat it", dials, dials)
	}
}
