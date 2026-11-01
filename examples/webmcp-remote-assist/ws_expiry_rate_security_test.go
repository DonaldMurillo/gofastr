package main

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"
)

// sweepExpired dropped an expired support credential from the map and left
// its sockets open until the next frame or the idle re-check, so a socket
// kept receiving the session after the credential it was admitted on had
// expired. The sweep now closes every socket that credential owns.
func TestSweepClosesExpiredSupportSockets(t *testing.T) {
	prev := wsRecheckEvery
	wsRecheckEvery = time.Hour // only the sweep may close it
	t.Cleanup(func() { wsRecheckEvery = prev })

	srv, a := newTestApp(t)
	s := a.createSession()
	cookie := supportLogin(t, srv)
	ws := strictDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	defer ws.conn.Close()
	strictDrainWS(ws, 200*time.Millisecond)

	a.mu.Lock()
	a.supporters[cookie.Value] = time.Now().Add(-time.Second)
	a.mu.Unlock()
	a.sweepExpired()

	start := time.Now()
	for ws.recvText(3*time.Second) != nil {
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("SECURITY: the support socket stayed open after the sweep expired its credential")
	}
}

// Every media frame took the shared mutex and fanned out an event, with no
// frame budget. Frames now pass a per-socket token bucket (battery/rtc's
// shape); a socket over it is closed.
func TestMediaFramesAreRateLimited(t *testing.T) {
	srv, a := newTestApp(t)
	s := a.createSession()
	cookie := supportLogin(t, srv)
	ws := strictDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	defer ws.conn.Close()
	strictDrainWS(ws, 200*time.Millisecond)

	a.mu.Lock()
	before := s.seq
	a.mu.Unlock()

	// One write of many frames, so a server close mid-flood cannot fail
	// the write half-way through the test's own framing.
	const flood = 1000
	var buf []byte
	for i := range flood {
		buf = append(buf, maskedText(t, fmt.Sprintf(`{"kind":"media","up":%t}`, i%2 == 0))...)
	}
	start := time.Now()
	_, _ = ws.conn.Write(buf)
	for ws.recvText(3*time.Second) != nil {
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatal("the socket stayed open through a frame flood")
	}
	a.mu.Lock()
	published := s.seq - before
	a.mu.Unlock()
	// The bucket admits its burst plus what it refilled while the flood
	// ran, and nothing more.
	limit := uint64(2*wsFramesPerSecond) + uint64(elapsed.Seconds()*wsFramesPerSecond) + 16
	if published > limit {
		t.Fatalf("%d of %d flood frames published in %v; want at most %d", published, flood, elapsed, limit)
	}
}

// maskedText frames payload as one masked client text frame.
func maskedText(t *testing.T, payload string) []byte {
	t.Helper()
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		t.Fatal(err)
	}
	if len(payload) >= 126 {
		t.Fatalf("payload too long for a short frame: %d", len(payload))
	}
	out := []byte{0x81, byte(0x80 | len(payload))}
	out = append(out, mask[:]...)
	for i := range len(payload) {
		out = append(out, payload[i]^mask[i%4])
	}
	return out
}
