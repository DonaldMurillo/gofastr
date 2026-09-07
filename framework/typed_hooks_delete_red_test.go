//go:build red

package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a typed hook whose payload cannot be coerced to the declared shape fails closed —
// the callback is never invoked and an error names the drift.
// Surfaces: framework/typed_hooks.go::OnBeforeDelete, OnAfterDelete.
// Finding: both Delete wrappers do `id, _ := data.(string)`, so a drifted payload (anything
// but a string) is silently coerced to "" and the callback is STILL invoked, returning nil.
// Create/Update fail closed on drift (TestTypedHook_TypeConfusedPayloadFailsClosed) and the
// List/Get wrappers emit "payload type = %T, want …" errors; the two Delete wrappers are the
// divergent surfaces. A hook that gates deletion on the record id runs with an empty id it
// never matched, treating the request as a no-op or mis-scoped allow.
// Fix direction: type-assert strictly and return a naming error on drift, mirroring the
// List/Get wrappers, with the callback left uninvoked.

// TestTypedHookDeleteTypeDriftFailsClosed: a non-string delete payload must
// surface as an error with the callback never invoked, on both Delete
// surfaces.
func TestTypedHookDeleteTypeDriftFailsClosed(t *testing.T) {
	surfaces := []struct {
		name string
		reg  func(app *App, fn func(context.Context, string) error)
		fire func(app *App) error
	}{
		{"OnBeforeDelete", func(a *App, fn func(context.Context, string) error) {
			OnBeforeDelete(a, "edges", fn)
		}, func(a *App) error {
			return a.HookRegistry("edges").ExecuteHooks(context.Background(), hook.BeforeDelete, 42)
		}},
		{"OnAfterDelete", func(a *App, fn func(context.Context, string) error) {
			OnAfterDelete(a, "edges", fn)
		}, func(a *App) error {
			return a.HookRegistry("edges").ExecuteHooks(context.Background(), hook.AfterDelete, 42)
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			var calls []string
			s.reg(app, func(_ context.Context, id string) error {
				calls = append(calls, id)
				return nil
			})
			err := s.fire(app)
			if err == nil {
				t.Errorf("SECURITY: [typedhook-delete-failopen] %s accepted a type-drifted payload (int id) as success; a hook gating deletes on the record id silently ran against a zero-valued \"\" id", s.name)
			}
			if len(calls) != 0 {
				t.Errorf("SECURITY: [typedhook-delete-failopen] %s invoked its callback with the coerced id %q instead of failing closed on drift", s.name, calls[0])
			}
		})
	}
}
