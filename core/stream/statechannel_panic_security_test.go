package stream

import (
	"encoding/json"
	"testing"
	"time"
)

// Pins: panic isolation at every host-pluggable callback — a panic in an
// app-supplied SnapshotSource.SnapshotFor / FilterEvent is swallowed by the
// StateChannel dispatch path, which drops that one snapshot/event and keeps
// serving (2026-09-06 adversarial pass, round 5).
// Property: panic isolation at every host-pluggable callback — a panic in an
// app-supplied SnapshotSource.SnapshotFor / FilterEvent must not escape the
// StateChannel's dispatch path.
// Surfaces: core/stream/state_channel.go::Run :281-287 (dispatches jobs with no
// recover), runSnapshot :299 (c.source.SnapshotFor), runEvent :381 → deliver :398
// (c.source.FilterEvent).
// Finding: production callers run host code on this loop (battery/rtc/room.go:464,
// examples/webmcp-remote-assist/session.go:232), so one panicking app callback
// kills the whole app server for every user. Every sibling dispatch loop in the
// repo already carries the net (log/semantic/notify/webhook/redis, App.Start
// hooks, cron leader, event bridge, acp loaders) — this one is missing it.
// Fix direction: per-callback recover in the dispatch path that swallows the
// panic, drops that one snapshot/event, and keeps the loop serving its other
// connections.
//
// Test shape (sanctioned pattern, cf. core-ui/di/nil_provider_red_test.go): the
// panic is driven through the unexported seams runEvent / runSnapshot SYN-
// CHRONOUSLY under the test's own recover-guard — never through the library's
// own Run goroutine, which today would kill the test binary (a crash, not a
// finding).

// scRedEvent is the event payload of the panic-probe source.
type scRedEvent struct {
	Action string `json:"action"`
}

// scRedSource is a SnapshotSource whose callbacks panic on demand: FilterEvent
// panics when the event's Action matches panicFilter, SnapshotFor panics when
// panicSnap is set. Everything else behaves normally.
type scRedSource struct {
	panicFilter string
	panicSnap   bool
}

func (s *scRedSource) SnapshotFor(role string) (string, uint64) {
	if s.panicSnap {
		panic("scRedSource: SnapshotFor boom")
	}
	return "steady", 1
}

func (s *scRedSource) FilterEvent(role string, ev scRedEvent) (any, bool) {
	if s.panicFilter != "" && ev.Action == s.panicFilter {
		panic("scRedSource: FilterEvent boom")
	}
	return ev, true
}

// scRedGuarded runs call under the test's own recover-guard: today the panic
// escapes the library's dispatch path all the way out here, which is the red
// result. Post-fix the library's per-callback recover swallows it before it
// can leave the seam.
func scRedGuarded(t *testing.T, seam string, call func()) (panicked any) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panicked = r
			t.Errorf("SECURITY: [statechannel-panicnet] %s panicked on a host-supplied SnapshotSource callback (%v) — Run's dispatch loop has no recover, and production callers (battery/rtc/room.go:464, examples/webmcp-remote-assist/session.go:232) run app code on this loop, so one panicking callback kills the whole app server for every user", seam, r)
		}
	}()
	call()
	return nil
}

func TestStateChannelRedPanicNet(t *testing.T) {
	// Leg 1: runEvent → deliver → source.FilterEvent panics on the payload.
	// A conn is registered directly under the channel's lock so runEvent has
	// someone to deliver to; Run is NOT started (no goroutine races, and the
	// panic must be observed synchronously, not as a binary crash).
	t.Run("FilterEvent panic does not escape runEvent", func(t *testing.T) {
		src := &scRedSource{panicFilter: "boom"}
		c := NewStateChannel[string, string, scRedEvent](src)
		conn := newChannelConn(4)
		c.mu.Lock()
		c.conns[conn] = "admin"
		c.mu.Unlock()
		t.Cleanup(func() { go conn.Close() })

		scRedGuarded(t, "runEvent (source.FilterEvent)", func() {
			c.runEvent(stateJob[string, string, scRedEvent]{
				typ:   "boom",
				event: scRedEvent{Action: "boom"},
			})
		})
	})

	// Leg 2: the same shape on the snapshot side — runSnapshot calls
	// source.SnapshotFor before anything else, so a panicking host source
	// takes down the loop on the very first Connect.
	t.Run("SnapshotFor panic does not escape runSnapshot", func(t *testing.T) {
		src := &scRedSource{panicSnap: true}
		c := NewStateChannel[string, string, scRedEvent](src)
		conn := newChannelConn(4)
		t.Cleanup(func() { go conn.Close() })

		scRedGuarded(t, "runSnapshot (source.SnapshotFor)", func() {
			c.runSnapshot(stateJob[string, string, scRedEvent]{
				kind: stateJobSnapshot,
				conn: conn,
				role: "admin",
				done: make(chan struct{}),
			})
		})
	})

	// Leg 3 (liveness proof, cheap): on the channel from leg 1 — whose
	// FilterEvent already panicked once — start the real (goroutine) path and
	// verify a normal event still reaches the connected conn. Post-fix this
	// pins that the net drops ONE event, not the loop.
	t.Run("channel still serves after a callback panic", func(t *testing.T) {
		src := &scRedSource{panicFilter: "boom"}
		c := NewStateChannel[string, string, scRedEvent](src)
		conn := newChannelConn(4)
		c.mu.Lock()
		c.conns[conn] = "admin"
		c.mu.Unlock()
		go c.Run()
		t.Cleanup(c.Stop)

		panicked := scRedGuarded(t, "runEvent (source.FilterEvent, pre-Run)", func() {
			c.runEvent(stateJob[string, string, scRedEvent]{
				typ:   "boom",
				event: scRedEvent{Action: "boom"},
			})
		})
		if panicked == nil {
			// Only meaningful once legs 1-2 are fixed; assert liveness then.
			c.Publish("healthy", scRedEvent{Action: "healthy"})
			select {
			case data := <-conn.sendBuffer:
				var env SequencedEnvelope[json.RawMessage]
				if err := json.Unmarshal(data, &env); err != nil {
					t.Fatalf("setup broken: decode envelope: %v", err)
				}
				if env.Type != "healthy" {
					t.Errorf("SECURITY: [statechannel-panicnet] post-panic envelope Type = %q, want %q — the loop is alive but delivering the wrong event", env.Type, "healthy")
				}
			case <-time.After(2 * time.Second):
				t.Errorf("SECURITY: [statechannel-panicnet] channel stopped serving after a recovered callback panic — a panic in one host callback must drop that one event, not the loop")
			}
		}
	})
}
