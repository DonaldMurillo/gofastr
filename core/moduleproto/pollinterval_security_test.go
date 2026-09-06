package moduleproto

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Property: a NEGATIVE caller-supplied duration must never fold onto the
// default arm of a `<= 0` check. WaitForReady tested pollInterval `<= 0`
// and substituted 50ms, so a negative interval (sign or unit error)
// silently became the FASTEST poll cadence — hammering the child —
// instead of being refused.
// Surfaces: WaitForReady (host-side warmup gate).
// Pins pollInterval <= 0 folding onto the 50ms default, found by the
// 2026-09-04 red-probe round; fixed in WaitForReady returning an error
// for pollInterval < 0 while 0 keeps the default.

func TestWaitForReadyNegativePollRejected(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()
	// A ready:true child is load-bearing: with the fold in place the
	// negative interval becomes the 50ms default, the poll succeeds, and
	// WaitForReady returns nil — exactly the silent pass this test pins.
	// (A child with no MethodReady handler would fail the Call and mask
	// the fold with an unrelated error.)
	if err := child.Handle(MethodReady, func(_ context.Context, _ json.RawMessage) (any, error) {
		return ReadyResult{Ready: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForReady(ctx, host, -time.Millisecond); err == nil {
		t.Fatal("WaitForReady: negative pollInterval silently folded onto the 50ms default")
	}
}

func TestWaitForReadyZeroPollKeepsDefault(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()
	if err := child.Handle(MethodReady, func(_ context.Context, _ json.RawMessage) (any, error) {
		return ReadyResult{Ready: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitForReady(ctx, host, 0); err != nil {
		t.Fatalf("zero pollInterval must keep the 50ms default and reach ready: %v", err)
	}
}
