package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Property: the host-side warmup path fails closed on hostile answers. A
// child (the untrusted party) that answers module.handshake or
// module.ready with a malformed, null, or error result gets a terminal
// error — the handshake never succeeds on a zero-value outcome, and
// WaitForReady surfaces a decode or wire error instead of spinning until
// the spawn deadline.
//
// handshake_test.go pins the per-field mismatch cases (digest, instance
// id, generation, proto, critical feature). These are the result-SHAPE
// cases that reach the decode path before any field comparison runs.
func TestHandshakeWarmupHostileAnswers(t *testing.T) {
	cfg := HandshakeConfig{
		Expected: HandshakeExpected{
			Name:              "demo",
			Version:           "1.0.0",
			SurfaceSHA256:     "deadbeef",
			DesiredGeneration: 7,
			InstanceID:        "nonce-xyz",
		},
		HostProto:    ProtoRange{Min: 1, Max: 1},
		HostCritical: []string{"frobber"},
	}
	runHandshake := func(t *testing.T, result any, retErr error) error {
		t.Helper()
		host, child, _, _, cleanup := newPeerPair(t, 0)
		defer cleanup()
		if err := child.Handle(MethodHandshake, func(_ context.Context, _ json.RawMessage) (any, error) {
			return result, retErr
		}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := Handshake(ctx, host, cfg)
		return err
	}

	t.Run("handshake non-object result", func(t *testing.T) {
		if err := runHandshake(t, json.RawMessage(`"nope"`), nil); err == nil {
			t.Fatal("a child answering module.handshake with a bare string must not handshake")
		}
	})
	t.Run("handshake null result", func(t *testing.T) {
		// json.Unmarshal(null, &struct{}) is NOT an error: without the
		// cross-check the host would accept a zero-value identity. The
		// round-trip check must catch it.
		if err := runHandshake(t, json.RawMessage(`null`), nil); err == nil {
			t.Fatal("a child answering module.handshake with null must not handshake")
		}
	})
	t.Run("handshake wire error", func(t *testing.T) {
		err := runHandshake(t, nil, errors.New("child exploded"))
		if err == nil {
			t.Fatal("a child answering module.handshake with an error must not handshake")
		}
		if AsError(err) == nil {
			t.Fatalf("wire error must survive as *Error, got %T (%v)", err, err)
		}
	})

	runReady := func(t *testing.T, result any, retErr error) error {
		t.Helper()
		host, child, _, _, cleanup := newPeerPair(t, 0)
		defer cleanup()
		if err := child.Handle(MethodReady, func(_ context.Context, _ json.RawMessage) (any, error) {
			return result, retErr
		}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return WaitForReady(ctx, host, 10*time.Millisecond)
	}

	t.Run("ready type-confused result", func(t *testing.T) {
		start := time.Now()
		if err := runReady(t, map[string]any{"ready": "yes"}, nil); err == nil {
			t.Fatal(`a child answering module.ready with ready:"yes" must fail the warmup gate`)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("type-confused ready answer spun for %v instead of failing closed", elapsed)
		}
	})
	t.Run("ready wire error", func(t *testing.T) {
		err := runReady(t, nil, errors.New("not ready ever"))
		if err == nil {
			t.Fatal("a child answering module.ready with an error must fail the warmup gate")
		}
		if AsError(err) == nil {
			t.Fatalf("wire error must surface as *Error, got %T (%v)", err, err)
		}
	})
}
