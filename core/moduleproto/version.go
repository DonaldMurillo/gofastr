package moduleproto

import (
	"fmt"
)

// ProtoRange is the inclusive integer protocol-version window a peer
// advertises in module.handshake.
type ProtoRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Protocol versions this package speaks.
const (
	ProtoV1    = 1
	ProtoV1Min = 1
	ProtoV1Max = 1
)

// DefaultProtoRange is the range a v1 Peer advertises.
var DefaultProtoRange = ProtoRange{Min: ProtoV1Min, Max: ProtoV1Max}

// Contains reports whether v falls in [r.Min, r.Max] inclusive.
func (r ProtoRange) Contains(v int) bool { return v >= r.Min && v <= r.Max }

// Validate returns ErrNegotiation unless 1 <= Min <= Max.
func (r ProtoRange) Validate() error {
	if r.Min > r.Max {
		return fmt.Errorf("%w: proto range min=%d > max=%d", ErrNegotiation, r.Min, r.Max)
	}
	if r.Min < 1 {
		return fmt.Errorf("%w: proto min=%d must be >= 1", ErrNegotiation, r.Min)
	}
	return nil
}

// Negotiate returns min(host.Max, child.Max) when it is at least
// max(host.Min, child.Min), and ErrNegotiation otherwise or for an invalid
// range.
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

// CheckCritical returns ErrCriticalFeature for the first required name not
// in supported.
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

// FeatureAck returns the names in both lists, in a's order, each once.
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
