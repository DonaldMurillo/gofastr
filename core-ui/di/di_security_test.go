package di

import (
	"sync"
	"testing"
)

// TestInjectConcurrentColdStart asserts that concurrent first-time
// injection of a func-provided dependency does not race on the
// container's internal maps (concurrent map writes are fatal & kill
// the process). Run with -race to also surface the data race.
func TestInjectConcurrentColdStart(t *testing.T) {
	type svc struct{ n int }
	type screen struct {
		S *svc `inject:""`
	}

	c := NewContainer()
	// Register as a func-constructor so the lazy branch in Inject runs
	// (a direct value would be pre-resolved and never write under RLock).
	if err := c.Provide(func() *svc { return &svc{n: 42} }); err != nil {
		t.Fatalf("Provide: %v", err)
	}

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			var sc screen
			if err := c.Inject(&sc); err != nil {
				t.Errorf("Inject: %v", err)
				return
			}
			if sc.S == nil || sc.S.n != 42 {
				t.Errorf("injected value wrong: %+v", sc.S)
			}
		}()
	}
	wg.Wait()
}

// TestInjectUnexportedFieldErrors pins that Inject reports an unexported
// inject-tagged field as an error instead of panicking. reflect.Value.Set
// on a value obtained from an unexported field panics, and the render
// pipeline calls Inject BEFORE component.SafeRenderCtx's recover
// (app.RenderPageResult), so a screen struct with one lowercased tagged
// field — an easy habit typo — takes the page down with an unrecovered
// panic on every request. Property: Inject must return an error for
// fields it cannot set, never panic. Surfaces: both Set sites, the
// resolved-singleton cache branch and the lazy provider branch.
func TestInjectUnexportedFieldErrors(t *testing.T) {
	type hidden struct {
		db *Database `inject:""` // unexported: reflect Set is refused
	}

	cached := NewContainer()
	if err := cached.Provide(&Database{DSN: "cached"}); err != nil {
		t.Fatalf("Provide: %v", err)
	}
	var warm *Database
	if err := cached.Resolve(&warm); err != nil {
		t.Fatalf("Resolve: %v", err)
	} // singleton now cached, Inject takes the fast branch

	lazy := NewContainer()
	if err := lazy.Provide(func() *Database { return &Database{DSN: "lazy"} }); err != nil {
		t.Fatalf("Provide: %v", err)
	}

	for _, tc := range []struct {
		name string
		c    *Container
	}{
		{"resolved-cache branch", cached},
		{"lazy-provider branch", lazy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SECURITY: [di-unexported] Inject panicked on an unexported inject-tagged field (%v) — Inject runs before the render pipeline's recover, so one lowercased field 500s the screen on every request instead of reporting the wiring error", r)
				}
			}()
			var h hidden
			if err := tc.c.Inject(&h); err == nil {
				t.Errorf("SECURITY: [di-unexported] Inject accepted an unsettable unexported field silently (err = nil)")
			}
		})
	}
}
