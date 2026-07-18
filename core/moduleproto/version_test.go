package moduleproto

import (
	"errors"
	"testing"
)

// TestNegotiatePicksMinOfMaxes: negotiated = min(host.max, child.max) when
// above the joint floor (design §4.5).
func TestNegotiatePicksMinOfMaxes(t *testing.T) {
	cases := []struct {
		name        string
		host, child ProtoRange
		want        int
	}{
		{"equal v1", ProtoRange{1, 1}, ProtoRange{1, 1}, 1},
		{"host higher max", ProtoRange{1, 3}, ProtoRange{1, 2}, 2},
		{"child higher max", ProtoRange{1, 2}, ProtoRange{1, 5}, 2},
		{"non-overlapping floors ok", ProtoRange{2, 4}, ProtoRange{1, 3}, 3},
		{"single version both", ProtoRange{5, 5}, ProtoRange{5, 5}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Negotiate(c.host, c.child)
			if err != nil {
				t.Fatalf("Negotiate err: %v", err)
			}
			if got != c.want {
				t.Fatalf("Negotiate = %d, want %d", got, c.want)
			}
		})
	}
}

// TestNegotiateRejectsEmptyIntersection: no overlap above floor = terminal.
func TestNegotiateRejectsEmptyIntersection(t *testing.T) {
	cases := []struct {
		name        string
		host, child ProtoRange
	}{
		{"disjoint ranges", ProtoRange{1, 2}, ProtoRange{3, 5}},
		{"disjoint reverse", ProtoRange{4, 5}, ProtoRange{1, 2}},
		{"touching but not overlapping floors", ProtoRange{1, 1}, ProtoRange{2, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Negotiate(c.host, c.child)
			if err == nil {
				t.Fatalf("expected negotiation failure")
			}
			if !errors.Is(err, ErrNegotiation) {
				t.Fatalf("error not ErrNegotiation: %v", err)
			}
		})
	}
}

// TestNegotiateRejectsBelowFloor: a peer whose min exceeds the other's max is
// rejected even if the min/max are individually valid.
func TestNegotiateRejectsBelowFloor(t *testing.T) {
	// host wants 5+, child caps at 3 → no overlap.
	_, err := Negotiate(ProtoRange{Min: 5, Max: 10}, ProtoRange{Min: 1, Max: 3})
	if err == nil {
		t.Fatalf("expected below-floor failure")
	}
	if !errors.Is(err, ErrNegotiation) {
		t.Fatalf("error not ErrNegotiation: %v", err)
	}
}

// TestNegotiateRejectsMalformedRange: min > max is invalid.
func TestNegotiateRejectsMalformedRange(t *testing.T) {
	_, err := Negotiate(ProtoRange{Min: 5, Max: 1}, ProtoRange{1, 1})
	if err == nil {
		t.Fatalf("expected malformed-range failure")
	}
	if !errors.Is(err, ErrNegotiation) {
		t.Fatalf("error not ErrNegotiation: %v", err)
	}
	// Min < 1 also invalid (versions are positive integers).
	_, err = Negotiate(ProtoRange{Min: 0, Max: 1}, ProtoRange{1, 1})
	if err == nil {
		t.Fatalf("expected min<1 failure")
	}
}

// TestCheckCriticalRejectsUnknown: a required feature not in supported = reject.
func TestCheckCriticalRejectsUnknown(t *testing.T) {
	err := CheckCritical([]string{"a", "b"}, []string{"a", "c"})
	if err == nil {
		t.Fatalf("expected critical-feature rejection")
	}
	if !errors.Is(err, ErrCriticalFeature) {
		t.Fatalf("error not ErrCriticalFeature: %v", err)
	}
}

// TestCheckCriticalAcceptsKnown: all required in supported = ok.
func TestCheckCriticalAcceptsKnown(t *testing.T) {
	if err := CheckCritical([]string{"a", "b", "c"}, []string{"a", "c"}); err != nil {
		t.Fatalf("unexpected critical-feature error: %v", err)
	}
	// Empty required = always ok.
	if err := CheckCritical(nil, nil); err != nil {
		t.Fatalf("empty required should be ok: %v", err)
	}
}

// TestFeatureAckIntersect: only names in both lists are acked.
func TestFeatureAckIntersect(t *testing.T) {
	got := FeatureAck([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	if len(got) != 2 {
		t.Fatalf("want 2 acked features, got %d (%v)", len(got), got)
	}
	set := map[string]bool{}
	for _, f := range got {
		set[f] = true
	}
	if !set["b"] || !set["c"] {
		t.Fatalf("expected b and c acked, got %v", got)
	}
}

// TestProtoRangeValidate exercises the structural check.
func TestProtoRangeValidate(t *testing.T) {
	if err := (ProtoRange{Min: 1, Max: 5}).Validate(); err != nil {
		t.Fatalf("valid range rejected: %v", err)
	}
	if err := (ProtoRange{Min: 5, Max: 1}).Validate(); err == nil {
		t.Fatalf("min>max should fail validate")
	}
}
