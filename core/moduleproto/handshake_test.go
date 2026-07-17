package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestHandshakeSuccessRoundTrip: a child that echoes the host's expected
// values, agrees on proto, and acks the host's critical features succeeds.
// Design §4.7 steps 3-4.
func TestHandshakeSuccessRoundTrip(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	// Child-side handshake handler: echo expected, ack proto + features.
	if err := child.Handle(MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
		var hp HandshakeParams
		if err := json.Unmarshal(p, &hp); err != nil {
			return nil, err
		}
		return HandshakeResult{
			Proto: ProtoRange{Min: 1, Max: 1},
			Identity: Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: hp.Expected.SurfaceSHA256,
			Features:      []string{"frobber", "widget"},
			Ready:         false,
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := Handshake(ctx, host, HandshakeConfig{
		Expected: HandshakeExpected{
			Name:              "demo",
			Version:           "1.0.0",
			ArtifactSHA256:    "abc",
			SurfaceSHA256:     "deadbeef",
			DesiredGeneration: 7,
			InstanceID:        "nonce-xyz",
		},
		Grants:       []string{"articles:read"},
		Limits:       DefaultLimits,
		HostProto:    ProtoRange{Min: 1, Max: 1},
		HostFeatures: []string{"frobber", "widget", "extra"},
		HostCritical: []string{"frobber"},
	})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if out.NegotiatedProto != 1 {
		t.Errorf("proto = %d, want 1", out.NegotiatedProto)
	}
	if out.Identity.InstanceID != "nonce-xyz" {
		t.Errorf("instance_id = %s", out.Identity.InstanceID)
	}
	if out.SurfaceSHA256 != "deadbeef" {
		t.Errorf("surface = %s", out.SurfaceSHA256)
	}
	// Acked features = intersection of host vs child.
	if len(out.AckedFeatures) != 2 {
		t.Errorf("acked = %v, want 2 items", out.AckedFeatures)
	}
}

// TestHandshakeDigestMismatchTerminal: a child that echoes a DIFFERENT
// surface digest is terminal — the round-trip must fail.
func TestHandshakeDigestMismatchTerminal(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
		var hp HandshakeParams
		_ = json.Unmarshal(p, &hp)
		return HandshakeResult{
			Proto: ProtoRange{Min: 1, Max: 1},
			Identity: Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: "DIFFERENT", // ← mismatch
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Handshake(ctx, host, HandshakeConfig{
		Expected:  HandshakeExpected{SurfaceSHA256: "expected", InstanceID: "x"},
		HostProto: ProtoRange{Min: 1, Max: 1},
	})
	if err == nil {
		t.Fatal("expected digest-mismatch error")
	}
	var hme *HandshakeMismatchError
	if !errors.As(err, &hme) {
		t.Fatalf("expected *HandshakeMismatchError, got %T: %v", err, err)
	}
	if hme.Field != "surface_sha256" {
		t.Errorf("field = %s, want surface_sha256", hme.Field)
	}
}

// TestHandshakeInstanceIDMismatch: a child echoing the wrong instance_id is
// terminal (the spawn-freshness anchor from §4.4).
func TestHandshakeInstanceIDMismatch(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodHandshake, func(_ context.Context, _ json.RawMessage) (any, error) {
		return HandshakeResult{
			Proto:         ProtoRange{Min: 1, Max: 1},
			Identity:      Identity{InstanceID: "STALE"},
			SurfaceSHA256: "x",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Handshake(ctx, host, HandshakeConfig{
		Expected:  HandshakeExpected{InstanceID: "FRESH", SurfaceSHA256: "x"},
		HostProto: ProtoRange{Min: 1, Max: 1},
	})
	if err == nil {
		t.Fatal("expected instance_id mismatch")
	}
	var hme *HandshakeMismatchError
	if !errors.As(err, &hme) || hme.Field != "identity.instance_id" {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestHandshakeGenerationMismatch: a stale desired_generation is terminal.
func TestHandshakeGenerationMismatch(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
		var hp HandshakeParams
		_ = json.Unmarshal(p, &hp)
		return HandshakeResult{
			Proto: ProtoRange{Min: 1, Max: 1},
			Identity: Identity{
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration - 1, // stale
			},
			SurfaceSHA256: hp.Expected.SurfaceSHA256,
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Handshake(ctx, host, HandshakeConfig{
		Expected:  HandshakeExpected{InstanceID: "x", SurfaceSHA256: "y", DesiredGeneration: 5},
		HostProto: ProtoRange{Min: 1, Max: 1},
	})
	if err == nil {
		t.Fatal("expected generation mismatch")
	}
	if !strings.Contains(err.Error(), "desired_generation") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestHandshakeProtoMismatchTerminal: no proto overlap is terminal.
func TestHandshakeProtoMismatchTerminal(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
		var hp HandshakeParams
		_ = json.Unmarshal(p, &hp)
		return HandshakeResult{
			Proto: ProtoRange{Min: 10, Max: 20}, // no overlap with host [1,1]
			Identity: Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: hp.Expected.SurfaceSHA256,
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Handshake(ctx, host, HandshakeConfig{
		Expected:  HandshakeExpected{InstanceID: "x", SurfaceSHA256: "y"},
		HostProto: ProtoRange{Min: 1, Max: 1},
	})
	if err == nil {
		t.Fatal("expected proto negotiation failure")
	}
	if !errors.Is(err, ErrNegotiation) {
		t.Fatalf("not ErrNegotiation: %v", err)
	}
}

// TestHandshakeCriticalMissingTerminal: a child that does NOT ack a
// host-critical feature is terminal.
func TestHandshakeCriticalMissingTerminal(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodHandshake, func(_ context.Context, p json.RawMessage) (any, error) {
		var hp HandshakeParams
		_ = json.Unmarshal(p, &hp)
		return HandshakeResult{
			Proto: ProtoRange{Min: 1, Max: 1},
			Identity: Identity{
				Name:              hp.Expected.Name,
				Version:           hp.Expected.Version,
				InstanceID:        hp.Expected.InstanceID,
				DesiredGeneration: hp.Expected.DesiredGeneration,
			},
			SurfaceSHA256: hp.Expected.SurfaceSHA256,
			Features:      []string{"only-this"}, // missing the critical one
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Handshake(ctx, host, HandshakeConfig{
		Expected:     HandshakeExpected{InstanceID: "x", SurfaceSHA256: "y"},
		HostProto:    ProtoRange{Min: 1, Max: 1},
		HostCritical: []string{"must-have"},
	})
	if err == nil {
		t.Fatal("expected critical-feature failure")
	}
	if !errors.Is(err, ErrCriticalFeature) {
		t.Fatalf("not ErrCriticalFeature: %v", err)
	}
}

// TestWaitForReadyPolls: WaitForReady returns nil once the child reports
// ready:true, polling in the interim.
func TestWaitForReadyPolls(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	var count int
	if err := child.Handle(MethodReady, func(_ context.Context, _ json.RawMessage) (any, error) {
		count++
		// Report ready only after the third poll.
		ready := count >= 3
		return ReadyResult{Ready: ready}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := WaitForReady(ctx, host, 25*time.Millisecond); err != nil {
		t.Fatalf("WaitForReady: %v", err)
	}
	if count < 3 {
		t.Errorf("expected at least 3 polls, got %d", count)
	}
}

// TestWaitForReadyDeadline: WaitForReady surfaces the ctx deadline.
func TestWaitForReadyDeadline(t *testing.T) {
	host, child, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	if err := child.Handle(MethodReady, func(_ context.Context, _ json.RawMessage) (any, error) {
		return ReadyResult{Ready: false}, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := WaitForReady(ctx, host, 25*time.Millisecond)
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong error: %v", err)
	}
}
