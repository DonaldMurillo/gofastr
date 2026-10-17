package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

var _ = registry.RegisterBehavior("noderender-probe", "(function(){})();",
	registry.Markers("[data-nr-probe]", `[data-nr-mode="x"]`))

// A registered behaviour binds whatever carries its marker, so an
// untrusted IR naming one arms a module on the host page: a button
// labelled "View invoice" that runs someone else's action.
func TestIRDropsBehaviorMarkers(t *testing.T) {
	props := map[string]any{"data-nr-probe": "/auth", "DATA-NR-MODE": "x", "data-nr-other": "kept", "text": "View invoice"}
	got := string(RenderKind("button", props, nil))
	if strings.Contains(strings.ToLower(got), "data-nr-probe") || strings.Contains(strings.ToLower(got), "data-nr-mode") {
		t.Errorf("untrusted IR kept a behaviour marker: %s", got)
	}
	if !strings.Contains(got, "data-nr-other") {
		t.Errorf("an unregistered data attribute was dropped too: %s", got)
	}
	if trusted := string(RenderTrustedKind("button", props, nil)); !strings.Contains(trusted, "data-nr-probe") {
		t.Errorf("trusted IR lost the marker: %s", trusted)
	}
}
