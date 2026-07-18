package moduleproto

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// HandshakeConfig is the host-side input to [Handshake]. Every opaque value
// here is CALLER-SUPPLIED — this package verifies the round-trip and emits a
// terminal [HandshakeMismatchError] on divergence; it makes NO trust decision
// about whether the expected values are themselves correct. That is the
// supervisor's policy (design §5 decision B).
type HandshakeConfig struct {
	// Expected is the set of values the child MUST echo back identically.
	// The host reads them from the operator-approved descriptor + the live
	// ProcessModuleStore (desired_generation) + its freshly-minted
	// instance_id (§4.7 step 1).
	Expected HandshakeExpected

	// Grants is the effective grant set the host is allowing this child
	// (descriptor.requested ∩ operator.approved).
	Grants []string

	// Limits is the per-child resource envelope the host enforces.
	Limits Limits

	// HostProto is the host's advertised proto range. The negotiated version
	// is min(host.Max, child.Max) if ≥ max(host.Min, child.Min); else
	// terminal. See [Negotiate].
	HostProto ProtoRange

	// HostFeatures is the host's non-critical feature list. The child may
	// ACK a subset; [HandshakeResult.AckedFeatures] is the intersection.
	HostFeatures []string

	// HostCritical is the host's REQUIRED feature list. Every name here
	// MUST appear in the child's echoed features[]; else terminal
	// ([ErrCriticalFeature]).
	HostCritical []string
}

// HandshakeOutcome is the result of a successful [Handshake]: the negotiated
// proto version, the cross-checked identity, and the agreed feature set.
type HandshakeOutcome struct {
	// NegotiatedProto is the single integer version both peers MUST use.
	NegotiatedProto int

	// Identity is the child's echoed identity, verified to round-trip the
	// host's [HandshakeConfig.Expected].
	Identity Identity

	// SurfaceSHA256 is the child's echoed surface digest, verified equal
	// to the host's expected value.
	SurfaceSHA256 string

	// Ready is the child's initial ready flag. The host polls [MethodReady]
	// separately to gate on warmup completion; Ready here is informational.
	Ready bool

	// AckedFeatures is the intersection of the host's HostFeatures and the
	// child's echoed features[].
	AckedFeatures []string
}

// Handshake performs design §4.7 steps 3-4: it issues module.handshake with
// the caller-supplied config, then validates the response by
//
//  1. cross-checking the child's echoed identity + surface digest against the
//     expected values (terminal [HandshakeMismatchError] on divergence);
//  2. negotiating the proto version (terminal [ErrNegotiation] on empty
//     intersection / below-floor);
//  3. checking every host-critical feature appears in the child's features
//     (terminal [ErrCriticalFeature] on miss).
//
// Handshake does NOT poll module.ready — that is a separate step (§4.7 step
// 4). The Ready field on the outcome is the child's initial flag; the
// supervisor drives the warmup poll via [MethodReady].
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
		return nil, fmt.Errorf("moduleproto: handshake call: %w", err)
	}
	var result HandshakeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("moduleproto: handshake decode: %w", err)
	}

	// (1) Cross-check digests and identity. Every field the host sent in
	// Expected MUST round-trip exactly.
	if err := crossCheck(cfg.Expected, result); err != nil {
		return nil, err
	}

	// (2) Negotiate proto.
	negotiated, err := Negotiate(cfg.HostProto, result.Proto)
	if err != nil {
		return nil, fmt.Errorf("moduleproto: %w", err)
	}

	// (3) Every host-critical feature must be in the child's echoed set.
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

// crossCheck enforces the round-trip identity/digest contract. The values in
// `want` are caller-supplied (host-side authoritative); the values in `got`
// are what the child echoed. A mismatch is terminal per design §4.5/§4.7.
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
	// instance_id is the per-spawn liveness nonce. A mismatch means this is
	// NOT the spawn the host just minted — reject as stale/duplicate.
	if err := check("identity.instance_id", want.InstanceID, got.Identity.InstanceID); err != nil {
		return err
	}
	// desired_generation is the persisted monotonic counter. A mismatch
	// means the child's view is stale relative to the store.
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

// WaitForReady polls module.ready until the child reports ready:true or until
// ctx is cancelled (design §4.7 step 4). It is the warmup gate. The poll
// interval defaults to 50ms if <= 0; callers should derive ctx from the 5s
// spawn deadline.
//
// WaitForReady returns nil on ready:true, ctx.Err() on deadline, or the
// underlying Call error on protocol fault.
func WaitForReady(ctx context.Context, p *Peer, pollInterval time.Duration) error {
	if pollInterval <= 0 {
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
