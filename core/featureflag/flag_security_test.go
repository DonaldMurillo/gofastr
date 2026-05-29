package featureflag

import (
	"context"
	"errors"
	"testing"
)

// errOnNthGet wraps a Store and returns an error on the Nth Get call,
// passing all other calls through to the inner store. This reproduces a
// transient store error (connection blip, pool exhaustion, lock timeout)
// that lands on a specific fetch in flight.
type errOnNthGet struct {
	inner Store
	fail  int // 1-based index of the Get call that should error
	calls int
}

func (s *errOnNthGet) Get(ctx context.Context, key string) (*Flag, error) {
	s.calls++
	if s.calls == s.fail {
		return nil, errors.New("featureflag: transient store error")
	}
	return s.inner.Get(ctx, key)
}

// TestBoolDefaultFailsClosed asserts that a kill switch wired with a
// safe-on fallback never fails open: a transient store error on ANY
// fetch must yield the supplied fallback, not the unsafe evaluated value.
func TestBoolDefaultFailsClosed(t *testing.T) {
	mem := NewMemoryStore()
	// A defined, enabled kill switch: when present and on, the protected
	// path is blocked. fallback=true means "block when we can't tell".
	if err := mem.Set(Flag{Key: "kill-payments", Enabled: true, Rollout: 100}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("happy path returns evaluated value", func(t *testing.T) {
		e := NewEvaluator(mem)
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("enabled flag should evaluate true")
		}
	})

	t.Run("error on first fetch returns fallback", func(t *testing.T) {
		e := NewEvaluator(&errOnNthGet{inner: mem, fail: 1})
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("store error must yield fallback=true, not fail open")
		}
	})

	t.Run("error on second fetch returns fallback", func(t *testing.T) {
		// This is the TOCTOU double-read: BoolDefault's own Get succeeds,
		// but the re-fetch inside Bool errors. Must still fail closed.
		e := NewEvaluator(&errOnNthGet{inner: mem, fail: 2})
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("error on second fetch must yield fallback=true, not false")
		}
	})

	t.Run("absent flag returns fallback", func(t *testing.T) {
		e := NewEvaluator(NewMemoryStore())
		if !e.BoolDefault(context.Background(), "kill-payments", true) {
			t.Fatal("absent flag must yield fallback=true")
		}
	})
}
