package main

import (
	"fmt"
	"testing"
	"time"
)

// The assist socket checked its role cookie once, at the upgrade: a support
// credential that expired or was revoked afterwards kept relaying signals
// and receiving the session until the browser left (2026-10-27 bug bash,
// unrechecked). An idle ticker and every inbound frame re-run the role
// check and close the socket.
func TestWSClosesRevokedSupport(t *testing.T) {
	t.Run("idle", func(t *testing.T) { testWSClosesRevokedSupport(t, 20*time.Millisecond, false) })
	t.Run("on frame", func(t *testing.T) { testWSClosesRevokedSupport(t, time.Hour, true) })
}

func testWSClosesRevokedSupport(t *testing.T, every time.Duration, frame bool) {
	prev := wsRecheckEvery
	wsRecheckEvery = every
	t.Cleanup(func() { wsRecheckEvery = prev })

	srv, a := newTestApp(t)
	s := a.createSession()
	cookie := supportLogin(t, srv)
	ws := strictDialWS(t, srv, "/support/session/"+s.id+"/ws", fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	defer ws.conn.Close()
	strictDrainWS(ws, 200*time.Millisecond)

	a.mu.Lock()
	delete(a.supporters, cookie.Value)
	a.mu.Unlock()
	if frame {
		ws.sendText(`{"kind":"media","up":true}`)
	}

	// recvText answers nil on a close frame, on EOF and on its deadline;
	// only the deadline takes the full wait.
	start := time.Now()
	for ws.recvText(3*time.Second) != nil {
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("SECURITY: the support socket stayed open after its credential was revoked")
	}
}
