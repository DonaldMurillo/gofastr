package effect

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: a request-time-evaluated response field can never put the
// process into a panic.
//
// respond_json's `status` is an expression string evaluated per request
// against the route scope (path, method), so its value is attacker-
// influenced even when the IR itself is benign. net/http's WriteHeader
// panics for any code outside 100..999, and kiln's applyRoutes installs
// raw http.HandlerFuncs — panic recovery in a kiln-rendered app is an
// opt-in catalog entry the world has to declare — so an out-of-range
// status is an uncontained panic on the serving goroutine.

// TestOutOfRangeStatusRejected pins that a status outside the HTTP range
// is refused at resolve time rather than handed to WriteHeader.
func TestOutOfRangeStatusRejected(t *testing.T) {
	for name, status := range map[string]any{
		"zero":         float64(0),
		"below 100":    float64(42),
		"above 999":    float64(1000),
		"negative":     float64(-1),
		"huge float":   1e30,
		"path length":  "len(ctx.path)", // benign IR + attacker-chosen path
		"non-integral": 200.5,
	} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"status": status},
		}
		scope := Scope{Ctx: map[string]any{"path": "/ab", "method": "GET"}}
		resp, err := Resolve(context.Background(), a, scope)
		if err != nil {
			continue // rejected at resolve — the preferred outcome
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s (status=%v) panicked in WriteTo: %v", name, status, r)
				}
			}()
			if wErr := resp.WriteTo(httptest.NewRecorder()); wErr != nil {
				t.Logf("%s: write error %v", name, wErr)
			}
		}()
	}
}

// TestInRangeStatusStillHonored guards against an over-strict clamp.
func TestInRangeStatusStillHonored(t *testing.T) {
	for _, want := range []int{200, 201, 302, 404, 418, 503} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"status": float64(want), "body": map[string]any{"ok": true}},
		}
		resp, err := Resolve(context.Background(), a, Scope{})
		if err != nil {
			t.Fatalf("status %d rejected: %v", want, err)
		}
		rec := httptest.NewRecorder()
		if err := resp.WriteTo(rec); err != nil {
			t.Fatalf("status %d: %v", want, err)
		}
		if rec.Code != want {
			t.Errorf("status = %d, want %d", rec.Code, want)
		}
	}
}
