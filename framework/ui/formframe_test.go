package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The frame is a plain layout fact: a container root, one inner grid,
// and a main and a side column. It adds no landmark and no semantics —
// the fields inside arrive with their own labels, and a region name
// would be read before each one.
func TestFormFrameRendersMainAndSide(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Main: []render.HTML{render.Text("main body")},
		Side: []render.HTML{render.Text("side body")},
	}))
	for _, want := range []string{
		`data-cui-comp="ui-form-frame"`,
		`class="fui-form-frame"`,
		`fui-form-frame__columns`,
		`fui-form-frame__main`,
		`fui-form-frame__side`,
		"main body",
		"side body",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	for _, banned := range []string{"role=", "aria-label", "<form", "<aside", "<section"} {
		if strings.Contains(h, banned) {
			t.Errorf("the frame is layout only, but the markup carries %q:\n%s", banned, h)
		}
	}
}

// Two columns only exist when the side column has something in it: an
// empty Side omits the column and drops the two-column modifier, so
// the main column takes the full width instead of waiting on a rail
// that never arrives.
func TestFormFrameEmptySideDropsTheColumn(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Main: []render.HTML{render.Text("solo")},
	}))
	if !strings.Contains(h, `fui-form-frame__main`) {
		t.Errorf("the main column is missing: %s", h)
	}
	if strings.Contains(h, "fui-form-frame__side") {
		t.Errorf("an empty Side must not render the column:\n%s", h)
	}
	if strings.Contains(h, "fui-form-frame--with-side") {
		t.Errorf("an empty Side must not arm the two-column modifier:\n%s", h)
	}
}

// An empty Main leaves the side column alone at full width, the mirror
// of the empty-Side case.
func TestFormFrameEmptyMainDropsTheColumn(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Side: []render.HTML{render.Text("solo")},
	}))
	if !strings.Contains(h, `fui-form-frame__side`) {
		t.Errorf("the side column is missing: %s", h)
	}
	if strings.Contains(h, "fui-form-frame__main") {
		t.Errorf("an empty Main must not render the column:\n%s", h)
	}
}

// The two-column threshold keys off the frame's own width, never the
// viewport's: the sheet sets container-type on the root and switches
// the grid inside an @container query, so the same form sits side by
// side on a full page and stacks in a drawer on a wide screen.
func TestFormFrameSwitchesOnItsOwnWidth(t *testing.T) {
	css := formFrameStyle.Entry().CSSFor(theme.Default())
	if !strings.Contains(css, "@container (min-width:") {
		t.Errorf("the two-column posture is not a container query:\n%s", css)
	}
	if strings.Contains(css, "@media") {
		t.Errorf("the frame must not key off the viewport:\n%s", css)
	}
	// The two-column track reads a knob with a fallback, so a host can
	// widen the side column without forking the sheet, and the sheet
	// stays inside the token/knob grammar the contracts check holds.
	if !strings.Contains(css, "var(--ui-form-frame-side") {
		t.Errorf("the side track does not read the side-width knob:\n%s", css)
	}
}

// SideWidth rides the root as one scoped custom property — the
// Workbench rail pattern — never an inline width, so strict-CSP pages
// stay clean and a malformed value is dropped, not shipped.
func TestFormFrameSideWidthIsAScopedKnob(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Side:      []render.HTML{render.Text("s")},
		SideWidth: "20rem",
	}))
	if !strings.Contains(h, `style="--ui-form-frame-side: 20rem"`) {
		t.Errorf("the side width did not land as the scoped knob:\n%s", h)
	}
	bad := string(FormFrame(FormFrameConfig{
		Side:      []render.HTML{render.Text("s")},
		SideWidth: "20rem; color: red",
	}))
	if strings.Contains(bad, "--ui-form-frame-side") {
		t.Errorf("a malformed SideWidth must be dropped, not shipped:\n%s", bad)
	}
}

// The usual seams: a caller's class joins the root's, an id lands, and
// ExtraAttrs cannot smuggle the style attribute the knob owns.
func TestFormFrameSeams(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Main:      []render.HTML{render.Text("m")},
		Side:      []render.HTML{render.Text("s")},
		ID:        "ff",
		Class:     "extra",
		SideWidth: "18rem",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "style": "display:none",
		},
	}))
	for _, want := range []string{`id="ff"`, `"fui-form-frame extra"`, `data-test="hook"`, `--ui-form-frame-side: 18rem`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	if strings.Contains(h, "display:none") {
		t.Errorf("a caller forged the style attribute the SideWidth knob owns:\n%s", h)
	}
}
