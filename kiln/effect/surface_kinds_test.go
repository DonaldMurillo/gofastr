package effect_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/effect"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// world.HookActionKinds and world.RouteActionKinds are what authoring
// admits per surface; this pins them to what Run and Resolve dispatch, so
// adding a kind to one side without the other fails here, not on the
// first request.
func TestSurfaceKindsMatchDispatch(t *testing.T) {
	for _, kind := range world.HookActionKinds {
		err := effect.Run(context.Background(), world.Action{Kind: kind}, effect.Scope{})
		if err != nil && strings.Contains(err.Error(), "unknown action kind") {
			t.Errorf("hook kind %q is admitted but effect.Run does not dispatch it", kind)
		}
	}
	for _, kind := range world.RouteActionKinds {
		_, err := effect.Resolve(context.Background(), world.Action{Kind: kind}, effect.Scope{})
		if err != nil && strings.Contains(err.Error(), "does not produce a response") {
			t.Errorf("route kind %q is admitted but effect.Resolve does not answer with it", kind)
		}
	}
}
