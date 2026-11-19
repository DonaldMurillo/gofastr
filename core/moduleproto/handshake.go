package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// HandshakeConfig is the host side of Handshake.
type HandshakeConfig struct {
	// Expected is what the child must echo back exactly.
	Expected HandshakeExpected

	// Grants is the effective grant set (requested ∩ approved).
	Grants []string

	Limits Limits

	HostProto ProtoRange

	// HostFeatures are offered; HostCritical must all be in the child's features.
	HostFeatures []string

	HostCritical []string
}

// HandshakeOutcome is what a successful Handshake agreed.
type HandshakeOutcome struct {
	NegotiatedProto int

	Identity Identity

	SurfaceSHA256 string

	// Ready is the child's initial flag; WaitForReady gates on warmup.
	Ready bool

	// AckedFeatures is FeatureAck(HostFeatures, the child's features).
	AckedFeatures []string
}

// Handshake sends module.handshake and returns HandshakeMismatchError,
// ErrNegotiation or ErrCriticalFeature for a reply that does not check out,
// and an error wrapping ErrHandshakeUnanswered when no reply came.
func Handshake(ctx context.Context, p *Peer, cfg HandshakeConfig) (*HandshakeOutcome, error) {
	params := HandshakeParams{
		Expected: cfg.Expected,
		Grants:   cfg.Grants,
		Limits:   cfg.Limits,
		Features: cfg.HostFeatures,
		Critical: cfg.HostCritical,
		Proto:    cfg.HostProto,
	}
	raw, err := p.Call(ctx, MethodHandshake, params)
	if err != nil {
		if _, answered := errors.AsType[*Error](err); answered {
			return nil, fmt.Errorf("moduleproto: handshake call: %w", err)
		}
		return nil, fmt.Errorf("moduleproto: handshake call: %w: %w", ErrHandshakeUnanswered, err)
	}
	var result HandshakeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("moduleproto: handshake decode: %w", err)
	}

	if err := crossCheck(cfg.Expected, result); err != nil {
		return nil, err
	}

	negotiated, err := Negotiate(cfg.HostProto, result.Proto)
	if err != nil {
		return nil, fmt.Errorf("moduleproto: %w", err)
	}

	if err := CheckCritical(result.Features, cfg.HostCritical); err != nil {
		return nil, fmt.Errorf("moduleproto: %w", err)
	}

	return &HandshakeOutcome{
		NegotiatedProto: negotiated,
		Identity:        result.Identity,
		SurfaceSHA256:   result.SurfaceSHA256,
		Ready:           result.Ready,
		AckedFeatures:   FeatureAck(cfg.HostFeatures, result.Features),
	}, nil
}

func crossCheck(want HandshakeExpected, got HandshakeResult) error {
	check := func(field, w, g string) error {
		if w != g {
			return &HandshakeMismatchError{Field: field, Want: w, Got: g}
		}
		return nil
	}
	if err := check("identity.name", want.Name, got.Identity.Name); err != nil {
		return err
	}
	if err := check("identity.version", want.Version, got.Identity.Version); err != nil {
		return err
	}
	if err := check("identity.instance_id", want.InstanceID, got.Identity.InstanceID); err != nil {
		return err
	}
	if want.DesiredGeneration != got.Identity.DesiredGeneration {
		return &HandshakeMismatchError{
			Field: "identity.desired_generation",
			Want:  fmt.Sprintf("%d", want.DesiredGeneration),
			Got:   fmt.Sprintf("%d", got.Identity.DesiredGeneration),
		}
	}
	if err := check("surface_sha256", want.SurfaceSHA256, got.SurfaceSHA256); err != nil {
		return err
	}
	return nil
}

// WaitForReady polls module.ready every pollInterval (0 = 50ms, negative
// is an error) until the child is ready, ctx ends or a call fails.
func WaitForReady(ctx context.Context, p *Peer, pollInterval time.Duration) error {
	if pollInterval < 0 {
		return fmt.Errorf("moduleproto: WaitForReady: pollInterval must be >= 0 (got %v)", pollInterval)
	}
	if pollInterval == 0 {
		pollInterval = 50 * time.Millisecond
	}
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		raw, err := p.Call(ctx, MethodReady, ReadyParams{})
		if err != nil {
			return err
		}
		var r ReadyResult
		if jErr := json.Unmarshal(raw, &r); jErr != nil {
			return fmt.Errorf("moduleproto: ready decode: %w", jErr)
		}
		if r.Ready {
			return nil
		}
	}
}
