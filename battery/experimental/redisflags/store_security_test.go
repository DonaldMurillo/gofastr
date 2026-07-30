package redisflags

import (
	"context"
	"encoding/json"
	"testing"
)

// Property: a bound the store enforces on write is enforced on read too.
//
// Set validates RolloutPct in [0,100], but Redis is shared state — another
// replica, an older binary, an ops script, or anyone with Redis access can
// write the key directly. Validating only on the write path is validating
// the one caller that was already going to be well-behaved. Consumers do
// `hash % 100 < RolloutPct`, so a stored 10000 is a silent 100% rollout and
// a stored -1 inverts the comparison.

// TestOutOfRangeRolloutRejectedOnRead pins the read side of the bound.
func TestOutOfRangeRolloutRejectedOnRead(t *testing.T) {
	for name, pct := range map[string]int{
		"far above 100": 10000,
		"just above":    101,
		"negative":      -1,
		"far negative":  -10000,
	} {
		s, fake := newBoundStore(t)
		fake.data["flag:"+name] = rawFlag(t, name, pct)
		got, err := s.Get(context.Background(), name)
		if err == nil {
			t.Errorf("%s: Get returned %+v, want rejection", name, got)
		}
		// List must not surface it either.
		flags, err := s.List(context.Background())
		if err != nil {
			t.Fatalf("%s: List: %v", name, err)
		}
		for _, f := range flags {
			if f.Key == name {
				t.Errorf("%s: List surfaced out-of-range flag %+v", name, f)
			}
		}
	}
}

// TestInRangeRolloutStillReadable guards against an over-strict bound.
func TestInRangeRolloutStillReadable(t *testing.T) {
	s, fake := newBoundStore(t)
	for _, pct := range []int{0, 1, 50, 100} {
		key := "ok"
		fake.data["flag:"+key] = rawFlag(t, key, pct)
		got, err := s.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("pct=%d rejected: %v", pct, err)
		}
		if got == nil || got.RolloutPct != pct {
			t.Errorf("pct=%d round-tripped as %+v", pct, got)
		}
	}
}

func newBoundStore(t *testing.T) (*Store, *fakeRedis) {
	t.Helper()
	fake := newFakeRedis()
	return New(fake, Config{}), fake
}

func rawFlag(t *testing.T, key string, pct int) string {
	t.Helper()
	b, err := json.Marshal(Flag{Key: key, Enabled: true, RolloutPct: pct})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
