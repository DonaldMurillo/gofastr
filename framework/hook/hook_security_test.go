package hook

// Adversarial tests for the HookRegistry execution engine: panic isolation
// at the runHookSafely boundary, first-error-stops ordering, snapshot
import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Property: ExecuteHooks runs hooks in REGISTRATION order and stops at the
// first error — a failing hook vetoes the operation, later hooks never run.
// ---------------------------------------------------------------------------

func TestExecuteHooksStopsAtFirstError(t *testing.T) {
	hr := NewHookRegistry()
	var order []string
	hr.RegisterHook(BeforeCreate, func(context.Context, any) error {
		order = append(order, "first")
		return nil
	})
	hr.RegisterHook(BeforeCreate, func(context.Context, any) error {
		order = append(order, "second")
		return errors.New("veto")
	})
	hr.RegisterHook(BeforeCreate, func(context.Context, any) error {
		order = append(order, "third")
		return nil
	})

	err := hr.ExecuteHooks(context.Background(), BeforeCreate, map[string]any{})
	if err == nil || err.Error() != "veto" {
		t.Fatalf("err = %v, want the second hook's error", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("execution order = %v, want [first second] (stop at first error)", order)
	}
}

// ---------------------------------------------------------------------------
// Property: a panicking hook NEVER escapes ExecuteHooks — every panic shape
// a third-party hook can throw is converted to an error, and the registry
// stays usable afterwards (the next ExecuteHooks runs normally).
// ---------------------------------------------------------------------------

// Package-level nils so the panic shapes below are runtime faults the
// analyzers cannot prove at compile time (that is the point: a hook
// author's real nil bug looks exactly like this).
var (
	nilIntForPanic *int
	nilMapForPanic map[string]string
)

func TestExecuteHooksRecoversPanicShapes(t *testing.T) {
	shapes := []struct {
		name string
		fire func()
	}{
		{"string", func() { panic("boom") }},
		{"error", func() { panic(errors.New("boom")) }},
		{"nil-deref", func() { _ = *nilIntForPanic }},
		{"nil-map-write", func() { nilMapForPanic["x"] = "y" }},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			hr := NewHookRegistry()
			hr.RegisterHook(AfterUpdate, func(context.Context, any) error {
				s.fire()
				return nil
			})
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("SECURITY: [hook] panic escaped ExecuteHooks: %v", r)
					}
				}()
				err = hr.ExecuteHooks(context.Background(), AfterUpdate, map[string]any{})
			}()
			if err == nil {
				t.Fatal("expected the recovered panic as an error")
			}

			// Registry still usable after the recovered panic: the panicking hook
			// keeps firing (converted to an error every time), and a healthy hook
			// registered on another type runs normally.
			ran := false
			hr.RegisterHook(AfterCreate, func(context.Context, any) error {
				ran = true
				return nil
			})
			if err := hr.ExecuteHooks(context.Background(), AfterCreate, map[string]any{}); err != nil {
				t.Fatalf("ExecuteHooks on a healthy type after a recovered panic: %v", err)
			}
			if !ran {
				t.Error("hook registered after a panic never ran (registry wedged)")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Property: HooksFor hands out a COPY — a caller mutating the returned slice
// (or a hook list captured mid-run) cannot inject into or remove from the
// registry's live execution set.
// ---------------------------------------------------------------------------

func TestHooksForReturnsDefensiveCopy(t *testing.T) {
	hr := NewHookRegistry()
	hr.RegisterHook(BeforeDelete, func(context.Context, any) error { return nil })

	got := hr.HooksFor(BeforeDelete)
	got[0] = func(context.Context, any) error {
		t.Error("SECURITY: [hook] injected hook via mutated HooksFor slice ran")
		return nil
	}

	if err := hr.ExecuteHooks(context.Background(), BeforeDelete, "id"); err != nil {
		t.Fatalf("ExecuteHooks: %v", err)
	}
	if live := hr.HooksFor(BeforeDelete); len(live) != 1 {
		t.Errorf("registry holds %d hooks after caller-side mutation, want 1", len(live))
	}
}

// ---------------------------------------------------------------------------
// Property: a hook may register another hook DURING execution (the doc's
// re-entrancy scenario — kiln's build-mode runtime does this against a live
// server). The run must not deadlock, must not run the newcomer mid-pass,
// and the newcomer must fire on the next ExecuteHooks.
// ---------------------------------------------------------------------------

func TestRegisterDuringExecuteIsSafe(t *testing.T) {
	hr := NewHookRegistry()
	newcomerRan := false
	release := make(chan struct{})
	started := make(chan struct{})

	var once sync.Once
	hr.RegisterHook(BeforeCreate, func(context.Context, any) error {
		once.Do(func() { close(started) })
		<-release // hold the snapshot run open while the registration lands
		return nil
	})

	done := make(chan error, 1)
	go func() {
		done <- hr.ExecuteHooks(context.Background(), BeforeCreate, map[string]any{})
	}()

	<-started
	hr.RegisterHook(BeforeCreate, func(context.Context, any) error {
		newcomerRan = true
		return nil
	})
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecuteHooks during concurrent registration: %v", err)
		}
	case <-waitDeadline():
		t.Fatal("SECURITY: [hook] ExecuteHooks deadlocked on registration mid-run")
	}
	if newcomerRan {
		t.Error("hook registered mid-pass ran in the SAME pass (snapshot semantics violated)")
	}
	if err := hr.ExecuteHooks(context.Background(), BeforeCreate, map[string]any{}); err != nil {
		t.Fatalf("second ExecuteHooks: %v", err)
	}
	if !newcomerRan {
		t.Error("mid-pass-registered hook never ran on the next pass")
	}
}

// ---------------------------------------------------------------------------
// Property: ExecuteHooks on a type with no registrations is a nil-error
// no-op — the common case for every entity without hooks.
// ---------------------------------------------------------------------------

func TestExecuteHooksUnregisteredTypeNoop(t *testing.T) {
	hr := NewHookRegistry()
	for _, typ := range []HookType{BeforeCreate, AfterCreate, BeforeUpdate, AfterUpdate, BeforeDelete, AfterDelete, BeforeList, AfterList, BeforeGet, AfterGet} {
		if err := hr.ExecuteHooks(context.Background(), typ, map[string]any{}); err != nil {
			t.Errorf("ExecuteHooks(%d) on empty registry = %v, want nil", typ, err)
		}
	}
}

// waitDeadline is the watchdog timeout for the re-entrancy test's select.
func waitDeadline() <-chan time.Time {
	return time.After(2 * time.Second)
}
