package harness

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// waitStored polls the session log until it holds n events.
func waitStored(t *testing.T, h *Harness, sess ids.SessionID, n int) []control.EventEnvelope {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var got []control.EventEnvelope
		var since uint64
		for {
			page, err := h.Sessions.EventsSince(context.Background(), sess, since, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(page) == 0 {
				break
			}
			got = append(got, page...)
			since = page[len(page)-1].ID
		}
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("stored %d events, want %d", len(got), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A sub-agent's events reach the session log, tagged with its id.
func TestSubAgentEventsPersisted(t *testing.T) {
	h := newShutdownTestHarness(t)
	defer func() { _ = h.Shutdown() }()
	sess := mustSession(t, h, h.Providers[0], "openrouter:test")
	bus := h.Mux.EngineFor(sess).Bus
	sub := engine.NewSubBus(bus, "client_sub")
	defer sub.Close()
	if _, err := sub.Publish(control.ToolCallStarted{Tool: "Bash"}, ""); err != nil {
		t.Fatal(err)
	}
	got := waitStored(t, h, sess, 1)
	if got[0].Kind != "ToolCallStarted" || got[0].SubAgent != "client_sub" {
		t.Fatalf("stored %+v, want the tagged sub-agent call", got[0])
	}
}

// The session bus reads its replay from the session log, so a client
// resuming from further back than the in-memory window still gets every
// event instead of a gap.
func TestSessionBusReplaysFromLog(t *testing.T) {
	h := newShutdownTestHarness(t)
	defer func() { _ = h.Shutdown() }()
	sess := mustSession(t, h, h.Providers[0], "openrouter:test")
	bus := h.Mux.EngineFor(sess).Bus
	const n = 1100 // past the bus's 1024-event window
	// In one burst: the persist loop's subscription is lossless, so a
	// burst past its 256-event buffer waits instead of dropping.
	for range n {
		if _, err := bus.Publish(control.TextDelta{Text: "x"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	waitStored(t, h, sess, n)
	backlog := bus.Backlog(context.Background(), 0, 0)
	if len(backlog) != n || backlog[0].ID != 1 {
		t.Fatalf("backlog has %d events starting %s/%d, want %d from id 1",
			len(backlog), backlog[0].Kind, backlog[0].ID, n)
	}
}
