package acp_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// plausibleSessionCap names the bound the server enforces per
// connection: acp.defaultSessionsPerConn (1024). An ACP client
// legitimately mints one session per task over a long-lived connection,
// so the bound is generous — the property under test is that SOME
// bound below "until the connection closes" exists, decided in the
// server (where a client cannot skip it), not left to the embedder's
// Agent.NewSession.
//
// Found red in the 2026-09-05 adversarial pass round 4: 10,000
// session/new frames on one zero-value-Options connection were all
// accepted (10,000 embedder Session objects retained plus their
// prompt-turn goroutine budget).

const plausibleSessionCap = 1024

func TestSessionMintingPerConnIsBounded(t *testing.T) {
	var n atomic.Int64
	agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
		return &fakeSession{id: fmt.Sprintf("s%d", n.Add(1))}, nil
	}}
	// Zero-value Options: no AuthMethods, no Authenticate — the
	// documented usable default, so every frame below is as cheap as
	// ACP gets, and the cap must come from the server itself.
	d := startDialog(t, agent, nil)
	d.initialize()

	// io.Pipe is unbuffered and the server answers inline, so the writes
	// must overlap the reads or both sides block once the frames channel
	// (buffered 64) fills: send from a goroutine, drain in the test.
	const flood = 2_048
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := range flood {
			d.request(100+i, "session/new", map[string]any{"cwd": "/tmp/p"})
		}
	}()

	// session/new is answered inline in read order, so exactly `flood`
	// frames answer the flood; count how many the server accepted.
	live := 0
	refused := 0
	for range flood {
		f := d.frame()
		if f["error"] == nil {
			live++
		} else {
			refused++
		}
	}
	<-sent
	if live != int(n.Load()) {
		t.Fatalf("harness mismatch: server accepted %d sessions, agent minted %d (a refused frame must not reach the embedder)", live, n.Load())
	}
	if live > plausibleSessionCap {
		t.Errorf("SECURITY: [acp-sessions] one connection minted %d live sessions with no bound (want ≤ %d per connection): the per-connection session cap must bound the live-session set for the connection's lifetime", live, plausibleSessionCap)
	}
	if refused != flood-plausibleSessionCap {
		t.Errorf("refused %d of %d session/new frames, want exactly %d past the %d cap", refused, flood, flood-plausibleSessionCap, plausibleSessionCap)
	}
}

// TestSessionCapEvictOldest pins the configurable overflow policy:
// past the cap the connection's OLDEST session is dropped (its in-flight
// prompt turn canceled) and the new one seated; the live set never
// exceeds the cap and every frame still succeeds.
func TestSessionCapEvictOldest(t *testing.T) {
	var n atomic.Int64
	agent := &fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
		return &fakeSession{id: fmt.Sprintf("e%d", n.Add(1))}, nil
	}}
	d := startDialog(t, agent, &acp.Options{
		MaxSessions:     2,
		SessionOverflow: acp.SessionOverflowEvictOldest,
	})
	d.initialize()

	const flood = 8
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := range flood {
			d.request(200+i, "session/new", map[string]any{"cwd": "/tmp/p"})
		}
	}()
	accepted := 0
	for range flood {
		f := d.frame()
		if f["error"] == nil {
			accepted++
		} else {
			t.Errorf("session/new past the cap under EvictOldest errored: %v", f["error"])
		}
	}
	<-sent
	if accepted != flood || int(n.Load()) != flood {
		t.Fatalf("harness mismatch: accepted %d, minted %d, want %d", accepted, n.Load(), flood)
	}

	// The live set is bounded at the cap: the oldest ids are gone, the
	// newest survive.
	d.request(300, "session/prompt", map[string]any{"sessionId": "e1", "prompt": []map[string]any{{"type": "text", "text": "hi"}}})
	f := d.frame()
	if f["error"] == nil {
		t.Errorf("prompt on the evicted oldest session (e1) succeeded; it must have been evicted past the cap")
	}
	d.request(301, "session/prompt", map[string]any{"sessionId": fmt.Sprintf("e%d", flood), "prompt": []map[string]any{{"type": "text", "text": "hi"}}})
	f = d.frame()
	if f["error"] != nil {
		t.Errorf("prompt on the newest session (e%d) failed: %v", flood, f["error"])
	}
}
