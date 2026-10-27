package render

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// world.HookEvents is what authoring admits; mapHookType is what the
// rebuild can register. They must name the same events.
func TestHookEventsAllMap(t *testing.T) {
	for _, when := range world.HookEvents {
		if _, err := mapHookType(when); err != nil {
			t.Errorf("world admits hook event %q but render cannot map it: %v", when, err)
		}
	}
	if _, err := mapHookType("whenever"); err == nil {
		t.Error("mapHookType accepted an unknown event")
	}
}
