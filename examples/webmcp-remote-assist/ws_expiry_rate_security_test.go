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
	srv, a := newTestApp(t)
	a.mu.Lock()
	a.recheckEvery = time.Hour // only the sweep may close it
	a.mu.Unlock()
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

// The sweep closed a credential's sockets one after another, and each
// Close waits up to a second for the peer's Close frame. With peers that
// never answer, the fourth socket kept receiving the session about three
// seconds after its credential expired. Every close now starts at once.
func TestSweepClosesSocketsTogether(t *testing.T) {
	srv, a := newTestApp(t)
	a.mu.Lock()
	a.recheckEvery = time.Hour // only the sweep may close them
	a.mu.Unlock()
	cookie := supportLogin(t, srv)
	const n = 4
	conns := make([]*strictWS, n)
	for i := range conns {
		s := a.createSession()
		conns[i] = strictDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
		defer conns[i].conn.Close()
		strictDrainWS(conns[i], 200*time.Millisecond)
	}

	a.mu.Lock()
	a.supporters[cookie.Value] = time.Now().Add(-time.Second)
	a.mu.Unlock()
	start := time.Now()
	a.sweepExpired()

	// recvText answers nil on the server's Close frame, which these
	// peers never answer, so every server-side Close waits its full
	// second for a reply.
	waits := make(chan time.Duration, n)
	for _, ws := range conns {
		go func() {
			for ws.recvText(5*time.Second) != nil {
			}
			waits <- time.Since(start)
		}()
	}
	for range n {
		if d := <-waits; d > 700*time.Millisecond {
			t.Fatalf("a socket got its Close %v after the sweep; want every close started at once", d)
		}
	}
}

// Every media frame took the shared mutex and fanned out an event, with no
// frame budget. Frames now pass a per-socket token bucket (battery/rtc's
// shape); a socket over it is closed. The limiter's clock is frozen and
// its rate small, so the bucket admits exactly its burst and the frame
// after it closes the socket, whatever the machine's speed.
func TestMediaFramesAreRateLimited(t *testing.T) {
	srv, a := newTestApp(t)
	const rate = 4
	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.mu.Lock()
	a.frameRate = rate
	a.frameClock = func() time.Time { return frozen }
	a.mu.Unlock()
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
	if time.Since(start) > 2*time.Second {
		t.Fatal("the socket stayed open through a frame flood")
	}
	a.mu.Lock()
	published := s.seq - before
	a.mu.Unlock()
	// Each admitted media frame publishes one event; with no refill the
	// bucket admits its burst (2x rate) and nothing more.
	if published != 2*rate {
		t.Fatalf("%d of %d flood frames published; want exactly the burst, %d", published, flood, 2*rate)
	}
}

// The bucket starts at its burst and refills at rate tokens a second.
func TestFrameLimiterRefillsAtRate(t *testing.T) {
	f := newFrameLimiter(4)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 8 {
		if !f.allow(t0) {
			t.Fatalf("frame %d refused inside the burst", i)
		}
	}
	if f.allow(t0) {
		t.Fatal("frame past the burst admitted with no time elapsed")
	}
	half := t0.Add(500 * time.Millisecond)
	if !f.allow(half) || !f.allow(half) || f.allow(half) {
		t.Fatal("half a second at 4/s should refill exactly 2 tokens")
	}
	if !f.allow(t0.Add(time.Hour)) {
		t.Fatal("an idle hour refilled nothing")
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
