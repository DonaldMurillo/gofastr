package component_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

type actionCallerKey struct{}

// A server-action handler receives a *ComponentContext and nothing else,
// so the request context has to be reachable through it — the endpoint's
// own contract says a handler that mutates anything must check
// authorization itself, and that check is unimplementable otherwise.
func TestComponentContext_CarriesRequestContext(t *testing.T) {
	base := context.WithValue(context.Background(), actionCallerKey{}, "caller-77")
	cc := component.NewComponentContextFor(base, "mutate", "target", map[string]string{"id": "42"})

	var asCtx context.Context = cc // must satisfy the interface
	if got := asCtx.Value(actionCallerKey{}); got != "caller-77" {
		t.Errorf("Value(ctxKey) = %v, want the request context's value", got)
	}
	if cc.EventName != "mutate" || cc.TargetID != "target" || cc.Param("id") != "42" {
		t.Errorf("event data lost: %+v", cc)
	}
}

// Cancellation and deadlines propagate, so a handler can pass the value
// it was given straight to a context-taking API.
func TestComponentContext_PropagatesCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	cc := component.NewComponentContextFor(base, "e", "", nil)

	select {
	case <-cc.Done():
		t.Fatal("Done fired before the request context was cancelled")
	default:
	}
	if err := cc.Err(); err != nil {
		t.Errorf("Err() = %v before cancel, want nil", err)
	}

	cancel()
	select {
	case <-cc.Done():
	case <-time.After(time.Second):
		t.Fatal("Done did not fire after the request context was cancelled")
	}
	if err := cc.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("Err() = %v after cancel, want context.Canceled", err)
	}
}

func TestComponentContext_PropagatesDeadline(t *testing.T) {
	want := time.Now().Add(time.Hour)
	base, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()

	cc := component.NewComponentContextFor(base, "e", "", nil)
	got, ok := cc.Deadline()
	if !ok || !got.Equal(want) {
		t.Errorf("Deadline() = (%v, %v), want (%v, true)", got, ok, want)
	}
}

// A ComponentContext built by hand — a test, an in-process invocation, or
// the older NewComponentContext — carries no request context. It must
// still be safe to use as one rather than panicking on a nil field.
func TestComponentContext_NilCtxBehavesAsBackground(t *testing.T) {
	for name, cc := range map[string]*component.ComponentContext{
		"NewComponentContext": component.NewComponentContext("e", "", nil),
		"zero value":          {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := cc.Deadline(); ok {
				t.Error("Deadline() reported one on a context-less ComponentContext")
			}
			if cc.Done() != nil {
				t.Error("Done() is non-nil; Background never completes and returns nil")
			}
			if err := cc.Err(); err != nil {
				t.Errorf("Err() = %v, want nil", err)
			}
			if v := cc.Value(actionCallerKey{}); v != nil {
				t.Errorf("Value(ctxKey) = %v, want nil", v)
			}
		})
	}
}

// State access is optional wiring; an unset getter/setter must no-op
// rather than panic, since the same value is handed to every handler.
func TestComponentContext_StateAccessorsOptional(t *testing.T) {
	cc := component.NewComponentContext("e", "", nil)
	if got := cc.GetState("missing"); got != nil {
		t.Errorf("GetState with no getter = %v, want nil", got)
	}
	cc.SetState("k", "v") // must not panic

	store := map[string]any{}
	cc.StateGetter = func(k string) any { return store[k] }
	cc.StateSetter = func(k string, v any) { store[k] = v }
	cc.SetState("k", "v")
	if got := cc.GetState("k"); got != "v" {
		t.Errorf("GetState after SetState = %v, want \"v\"", got)
	}
}

func TestComponentContext_ParamHelpers(t *testing.T) {
	cc := component.NewComponentContext("e", "", map[string]string{"n": "7", "bad": "x"})
	if got := cc.Param("n"); got != "7" {
		t.Errorf("Param(n) = %q, want \"7\"", got)
	}
	if got := cc.Param("absent"); got != "" {
		t.Errorf("Param(absent) = %q, want empty", got)
	}
	if got, err := cc.ParamInt("n"); err != nil || got != 7 {
		t.Errorf("ParamInt(n) = (%d, %v), want (7, nil)", got, err)
	}
	if _, err := cc.ParamInt("bad"); err == nil {
		t.Error("ParamInt on a non-numeric value returned no error")
	}
}
