package framework

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A panic inside a third-party or buggy hook must NOT propagate out of
// HookRegistry.ExecuteHooks. Without recovery, the http stack unwinds
// unpredictably and the surrounding tx leaks. The framework's
// lifecycle (tx rollback, error chain) handles errors deterministically;
// recovery converts panics into errors so the same code path applies.

var panicShapes = []struct {
	name string
	fire func()
}{
	{"string", func() { panic("boom") }},
	{"error", func() { panic(errors.New("boom")) }},
	{"struct", func() { panic(struct{ Code int }{500}) }},
	{"nil-deref", func() { var p *int; _ = *p }},
	{"index-out-of-range", func() { var s []string; _ = s[1] }},
	{"nil-map-write", func() { var m map[string]string; m["x"] = "y" }},
}

func TestHooks_RecoverPanicsAsErrors(t *testing.T) {
	for _, shape := range panicShapes {
		t.Run(shape.name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
				shape.fire()
				return nil
			})

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic escaped ExecuteHooks: %v", r)
				}
			}()
			err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, map[string]any{"title": "hi"})
			if err == nil {
				t.Fatalf("expected error from hook panic")
			}
		})
	}
}

// TestHooks_AfterRecoveryStillCallsLater ensures recovery doesn't break
// the existing first-error-stops loop semantic — recovered panics
// behave like any other error.
func TestHooks_AfterRecoveryStillCallsLater(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	calls := 0
	OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
		calls++
		panic("boom")
	})
	OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
		calls++
		return nil
	})

	err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, map[string]any{"title": "hi"})
	if err == nil {
		t.Fatalf("expected error from panicking hook")
	}
	if calls != 1 {
		t.Fatalf("expected loop to stop at first failing hook; got %d calls", calls)
	}
}
