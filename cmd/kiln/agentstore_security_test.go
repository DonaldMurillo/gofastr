package main

import (
	"context"
	"testing"
)

// Pins [stale-clear-orphans-cancel], found by the 2026-09-04 red-probe
// round; fixed in adapter_store.go ClearTurnCancel taking the turn's own
// registration token and clearing only while it still owns the slot.
// Property: a turn goroutine finishing late must clear only its own
// in-flight registration; a stale completion must never orphan the live
// turn's cancel hook (stop button / supersede / agent-switch all route
// through it).
// Surfaces: cmd/kiln/adapter_store.go::ClearTurnCancel (ownership check),
// cmd/kiln/agent_watcher.go::runAgentWatcher turn goroutine (passes the
// token its SetTurnCancel returned).
func TestStaleTurnClearKeepsLiveCancel(t *testing.T) {
	store := NewAdapterStore(Adapter{Name: "test"})

	// Turn A starts.
	ctxA, cancelA := context.WithCancelCause(context.Background())
	defer cancelA(nil)
	regA := store.SetTurnCancel(cancelA)

	// A newer chat_user supersedes A: SetTurnCancel cancels A and
	// registers turn B's hook. This is the exact watcher sequence
	// (agent_watcher.go).
	ctxB, cancelB := context.WithCancelCause(context.Background())
	defer cancelB(nil)
	regB := store.SetTurnCancel(cancelB)

	// A's context is cancelled synchronously by SetTurnCancel; its
	// goroutine observes the cancellation, drains, and then runs its
	// ownership-checked ClearTurnCancel.
	<-ctxA.Done()
	store.ClearTurnCancel(regA)
	if !store.InFlight() {
		t.Fatalf("stale clear from turn A ended the LIVE turn B's registration")
	}

	// The stale clear must not have consumed B's token either: clearing
	// with B's own token still works (the normal drain path).
	store.ClearTurnCancel(regB)
	if store.InFlight() {
		t.Fatalf("own-token clear did not end the turn")
	}

	// Re-run the supersede sequence, this time checking the stop button
	// path the red probe pinned: after A drains, CancelInFlight must
	// still cancel the live turn B.
	regA = store.SetTurnCancel(cancelA)
	regB = store.SetTurnCancel(cancelB)
	<-ctxA.Done()
	store.ClearTurnCancel(regA)
	if !store.CancelInFlight() {
		t.Fatalf("SECURITY: [stale-clear-orphans-cancel] after superseded turn A drained and cleared, CancelInFlight can no longer cancel the LIVE turn B: the stop button is dead and the agent turn is uncancellable. ClearTurnCancel must be ownership-checked.")
	}
	select {
	case <-ctxB.Done():
		// correct: the live turn was cancelled
	default:
		t.Fatalf("SECURITY: [stale-clear-orphans-cancel] live turn B was not cancelled by CancelInFlight after a stale turn cleared the shared hook")
	}
}
