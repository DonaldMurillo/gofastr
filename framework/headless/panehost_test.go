package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderPaneHost(p PaneHostProps) string { return string(PaneHost(p, nil)) }

func TestPaneHostRendersPrimaryAndSidePanes(t *testing.T) {
	h := renderPaneHost(PaneHostProps{
		Primary:        render.Text("The list"),
		Secondary:      render.Text("Detail"),
		SecondaryLabel: "Details",
		Tertiary:       render.Text("Inspector"),
	})
	for _, want := range []string{
		`<div data-hui-pane="primary">The list</div>`,
		`<div aria-label="Details" data-hui-pane="secondary" hidden="" role="region">Detail</div>`,
		`<div aria-label="Tertiary" data-hui-pane="tertiary" hidden="" role="region">Inspector</div>`,
		`data-hui-panehost=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pane host missing %q:\n%s", want, h)
		}
	}
}

func TestPaneHostOpenStateShipsServerRendered(t *testing.T) {
	h := renderPaneHost(PaneHostProps{
		Primary:       render.Text("p"),
		Secondary:     render.Text("s"),
		SecondaryOpen: true,
		DeepLinkParam: "pane",
	})
	if !strings.Contains(h, `data-hui-pane-open="secondary"`) {
		t.Errorf("the open modifier must ship in the markup:\n%s", h)
	}
	if strings.Contains(h, `data-hui-pane="secondary" hidden`) {
		t.Errorf("an open pane must not carry hidden:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-pane-deeplink="pane"`) {
		t.Errorf("the deep-link parameter must ride the root:\n%s", h)
	}
}

func TestPaneHostRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    PaneHostProps
	}{
		{"no primary", PaneHostProps{}},
		{"whitespace secondary label", PaneHostProps{Primary: render.Text("p"), Secondary: render.Text("s"), SecondaryLabel: " "}},
		{"empty deeplink token", PaneHostProps{Primary: render.Text("p"), DeepLinkParam: " "}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			PaneHost(tc.p, nil)
		}()
	}
	// A pane marked open with no pane is silently closed (the pane is
	// absent), not refused: open state without a pane is a config
	// no-op the render already repairs.
	h := renderPaneHost(PaneHostProps{Primary: render.Text("p"), SecondaryOpen: true})
	if strings.Contains(h, "data-hui-pane-open") {
		t.Errorf("an open state with no pane must not mark the host open:\n%s", h)
	}
}
