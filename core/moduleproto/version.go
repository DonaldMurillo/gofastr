package moduleproto

import (
	"fmt"
)

// ProtoRange is the integer protocol-version window a peer advertises in
// module.handshake (design §4.5). Versions are plain integers — there is no
// MCP-style "YYYY-MM-DD" string and no semver. [Min, Max] is inclusive.
//
// A peer that speaks exactly one version sets Min == Max. The floor for v1 of
// this package is [ProtoV1Min, ProtoV1Max].
type ProtoRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Protocol versions understood by this package. Bump ProtoV1Max when a new
// version is added; [Negotiate] picks the highest mutually-supported one.
const (
	// ProtoV1 is the single v1 protocol version number.
	ProtoV1 = 1
	// ProtoV1Min is the lowest version this package's Peer can speak.
	ProtoV1Min = 1
	// ProtoV1Max is the highest version this package's Peer can speak.
	ProtoV1Max = 1
)

// DefaultProtoRange is the range advertised by a v1 moduleproto Peer that has
// not been narrowed by configuration.
var DefaultProtoRange = ProtoRange{Min: ProtoV1Min, Max: ProtoV1Max}

// Contains reports whether v falls in [r.Min, r.Max] inclusive.
func (r ProtoRange) Contains(v int) bool { return v >= r.Min && v <= r.Max }

// Validate enforces the structural invariant Min ≤ Max. A malformed range is
// treated as a terminal handshake fault by [Negotiate].
func (r ProtoRange) Validate() error {
	if r.Min > r.Max {
		return fmt.Errorf("%w: proto range min=%d > max=%d", ErrNegotiation, r.Min, r.Max)
	}
	if r.Min < 1 {
		return fmt.Errorf("%w: proto min=%d must be >= 1", ErrNegotiation, r.Min)
	}
	return nil
}

// Negotiate implements the version intersection rule from design §4.5:
//
//	negotiated = min(host.Max, child.Max)
//	             if that value >= max(host.Min, child.Min)
//	             else terminal
//
// Both ranges are validated first; a malformed range is terminal. The returned
// version is what both peers MUST use for the rest of the connection — it is
// the highest mutually-supported version that is also at or above the joint
// floor (the stricter of the two minimums).
//
// Negotiate is pure — it makes no decision about feature policy; see
// [CheckCritical] for the critical-feature half of negotiation.
func Negotiate(host, child ProtoRange) (int, error) {
	if err := host.Validate(); err != nil {
		return 0, err
	}
	if err := child.Validate(); err != nil {
		return 0, err
	}
	ceiling := min(host.Max, child.Max)
	floor := max(host.Min, child.Min)
	if ceiling < floor {
		return 0, fmt.Errorf("%w: host=%v child=%v (ceiling %d < floor %d)",
			ErrNegotiation, host, child, ceiling, floor)
	}
	return ceiling, nil
}

// CheckCritical implements the critical-feature half of §4.5 negotiation.
//
// In the handshake, a peer may declare names in `critical` for features it
// REQUIRES the other endpoint to support. The receiver rejects the handshake
// if any of the sender's critical names are absent from its own supported set
// (`supported`). Unknown non-critical fields are ignored — only critical names
// force rejection.
//
// For v1 only the host→child direction is wired (the handshake result has no
// critical field). The supervisor calls CheckCritical(hostSupported, childCritical)
// when validating a child-declared critical set if/when one is added.
func CheckCritical(supported, required []string) error {
	if len(required) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(supported))
	for _, s := range supported {
		set[s] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			return fmt.Errorf("%w: %q not in supported set", ErrCriticalFeature, r)
		}
	}
	return nil
}

// FeatureAck selects the intersection of two peers' advertised non-critical
// feature lists. This is the agreed feature set both sides may rely on. Names
// not in both lists are simply not available; they are not errors.
func FeatureAck(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	out := make([]string, 0, min(len(a), len(b)))
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		if _, ok := set[s]; !ok {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
