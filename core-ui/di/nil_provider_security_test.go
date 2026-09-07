package di

import (
	"testing"
)

// Pins: Inject/Resolve return a descriptive error, never panic, for values they cannot set.
// (2026-09-06 adversarial pass, round 5.)
// Surfaces: di.go::Container.Inject lazy branch (reflect.ValueOf(nil) → Set panics), the
// resolved-cache branch, and ::Container.Resolve (which also caches the poison via
// resolved[targetType]=true before the panicking Set).
// Finding: a provider returning a nil INTERFACE yields result == nil (untyped nil any);
// reflect.ValueOf(result) is an invalid Value and ev.Field(i).Set(...) / tv.Elem().Set(...)
// panic — after the nil singleton was already cached. Inject runs before the render
// pipeline's recover (app.RenderPageResult), so one bad screen struct 500s every request.
// The unexported-field trigger is pinned (TestInjectUnexportedFieldErrors); the
// nil-interface-singleton trigger is not — a POINTER-nil provider sets fine (Set accepts a
// typed nil pointer), only the interface kind panics, so the behaviour is accidental.
// Fix direction: reject an invalid (zero) ValueOf(result) — or a nil interface result —
// with a wiring error before the Set, without writing singletons/resolved.

type redNilSvc interface{ N() int }

type redNilImpl struct{ n int }

func (i redNilImpl) N() int { return i.n }

type redNilHolder struct {
	S redNilSvc `inject:""`
}

// redNilCall runs f under the sibling recover-guard shape: a panic becomes a
// SECURITY failure instead of killing the binary, so the panic is observable
// as a red result. Returns (err, panicked).
func redNilCall(t *testing.T, call string, f func() error) (err error, panicked any) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panicked = r
			t.Errorf("SECURITY: [di-nil-provider-panic] %s panicked (%v) on a nil interface singleton — Inject runs before the render pipeline's recover, so one nil provider 500s the screen on every request instead of reporting the wiring error", call, r)
		}
	}()
	return f(), nil
}

func TestDiRedNilProviderErrors(t *testing.T) {
	t.Run("lazy-provider branch", func(t *testing.T) {
		c := NewContainer()
		if err := c.Provide(func() redNilSvc { return nil }); err != nil {
			t.Fatalf("setup broken: Provide(nil-interface constructor): %v", err)
		}
		var h redNilHolder
		err, panicked := redNilCall(t, "Inject (lazy-provider branch)", func() error { return c.Inject(&h) })
		if panicked == nil && err == nil {
			t.Errorf("SECURITY: [di-nil-provider-panic] Inject silently accepted a nil interface singleton (err = nil, no panic) — the injected field stays a nil interface the screen cannot distinguish from an unwired one")
		}
	})

	t.Run("resolve does not poison the cache", func(t *testing.T) {
		c := NewContainer()
		if err := c.Provide(func() redNilSvc { return nil }); err != nil {
			t.Fatalf("setup broken: Provide(nil-interface constructor): %v", err)
		}
		var out redNilSvc
		err, panicked := redNilCall(t, "Resolve", func() error { return c.Resolve(&out) })
		if panicked == nil && err == nil {
			t.Errorf("SECURITY: [di-nil-provider-panic] Resolve silently accepted a nil interface singleton (err = nil, no panic)")
		}
		// Resolve writes singletons/resolved BEFORE the panicking Set, so the
		// poison outlives the panic: a corrected later provider must still be
		// injectable on this container.
		if err := c.Provide(func() redNilSvc { return redNilImpl{n: 7} }); err != nil {
			t.Fatalf("setup broken: corrected Provide: %v", err)
		}
		var h redNilHolder
		herr, hpanicked := redNilCall(t, "Inject after corrected Provide", func() error { return c.Inject(&h) })
		if hpanicked == nil && herr != nil {
			t.Errorf("SECURITY: [di-nil-provider-panic] Resolve poisoned the resolved cache (resolved[type]=true with a nil singleton cached) — a corrected provider no longer injects: %v", herr)
		}
		if hpanicked == nil && herr == nil && (h.S == nil || h.S.N() != 7) {
			t.Errorf("SECURITY: [di-nil-provider-panic] corrected provider injected the wrong value: %+v", h.S)
		}
	})

	t.Run("resolved-cache branch", func(t *testing.T) {
		c := NewContainer()
		if err := c.Provide(func() redNilSvc { return nil }); err != nil {
			t.Fatalf("setup broken: Provide(nil-interface constructor): %v", err)
		}
		// Prime: the lazy branch caches the nil singleton (di.go writes
		// singletons/resolved at 151-152) BEFORE the panicking Set (153), so
		// the second Inject takes the resolved-cache branch.
		var prime redNilHolder
		_, _ = redNilCall(t, "Inject (prime, lazy branch)", func() error { return c.Inject(&prime) })
		var warm redNilHolder
		err, panicked := redNilCall(t, "Inject (resolved-cache branch)", func() error { return c.Inject(&warm) })
		if panicked == nil && err == nil {
			t.Errorf("SECURITY: [di-nil-provider-panic] Inject served the cached nil interface singleton silently (err = nil, no panic)")
		}
	})
}
