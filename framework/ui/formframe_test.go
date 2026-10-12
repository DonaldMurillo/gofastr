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

// SideWidth names one of the kit's rail widths: the name rides the root
// as a modifier class whose registered CSS sets the scoped
// --ui-form-frame-side knob from a --ui-form-frame-side-narrow / -wide
// token — never an inline style attribute, which the default CSP
// strips, and never a free-form length from caller config.
func TestFormFrameSideWidthIsANamedModifier(t *testing.T) {
	narrow := string(FormFrame(FormFrameConfig{
		Side:      []render.HTML{render.Text("s")},
		SideWidth: FormFrameSideNarrow,
	}))
	if !strings.Contains(narrow, `fui-form-frame--side-narrow`) {
		t.Errorf("the narrow width did not land as its modifier:\n%s", narrow)
	}
	wide := string(FormFrame(FormFrameConfig{
		Side:      []render.HTML{render.Text("s")},
		SideWidth: FormFrameSideWide,
	}))
	if !strings.Contains(wide, `fui-form-frame--side-wide`) {
		t.Errorf("the wide width did not land as its modifier:\n%s", wide)
	}
	def := string(FormFrame(FormFrameConfig{
		Side:      []render.HTML{render.Text("s")},
		SideWidth: FormFrameSideDefault,
	}))
	if strings.Contains(def, "fui-form-frame--side-") {
		t.Errorf("the default width must not ship a modifier:\n%s", def)
	}
	// The registered sheet backs every name with a token-reading rule;
	// an unknown name is a programming error and panics at render.
	css := formFrameStyle.Entry().CSSFor(theme.Default())
	for _, want := range []string{
		"--ui-form-frame-side: var(--ui-form-frame-side-narrow, 12rem)",
		"--ui-form-frame-side: var(--ui-form-frame-side-wide, 22rem)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the sheet lacks the named-width rule %q:\n%s", want, css)
		}
	}
	defer func() { recover() }()
	FormFrame(FormFrameConfig{Side: []render.HTML{render.Text("s")}, SideWidth: "18rem"})
	t.Error("an unknown SideWidth must panic at render")
}

// The usual seams: a caller's class joins the root's, an id lands, and
// ExtraAttrs cannot smuggle a style attribute — no surface of this
// component ever ships one, so the default CSP never strips part of it.
func TestFormFrameSeams(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Main:      []render.HTML{render.Text("m")},
		Side:      []render.HTML{render.Text("s")},
		ID:        "ff",
		Class:     "extra",
		SideWidth: FormFrameSideNarrow,
		ExtraAttrs: map[string]string{
			"data-test": "hook", "style": "display:none",
		},
	}))
	for _, want := range []string{`id="ff"`, `fui-form-frame extra`, `data-test="hook"`, `fui-form-frame--side-narrow`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	if strings.Contains(h, "display:none") {
		t.Errorf("a caller forged a style attribute:\n%s", h)
	}
	// No spelling of the attribute may ship, whatever the config: the
	// default CSP strips it before paint, so its presence would be a
	// silently broken layout, not a style.
	for _, cfg := range []FormFrameConfig{
		{Main: []render.HTML{render.Text("m")}, Side: []render.HTML{render.Text("s")}},
		{Side: []render.HTML{render.Text("s")}, SideWidth: FormFrameSideNarrow},
		{Side: []render.HTML{render.Text("s")}, SideWidth: FormFrameSideWide},
	} {
		if out := string(FormFrame(cfg)); strings.Contains(out, "style=") {
			t.Errorf("the frame shipped a style attribute the default CSP strips:\n%s", out)
		}
	}
}

// SidePanel draws the side column as a bordered surface that stays in
// view beside a long main column, the prototype's record rail. Sticky
// rides only the side-by-side posture: stacked under the fields in a
// drawer, a pinned panel would cover them. The main column holds a
// readable measure either way.
func TestFormFrameSidePanelSticksBesideFields(t *testing.T) {
	h := string(FormFrame(FormFrameConfig{
		Main:      []render.HTML{render.Text("m")},
		Side:      []render.HTML{render.Text("s")},
		SidePanel: true,
	}))
	if !strings.Contains(h, `fui-form-frame--side-panel`) {
		t.Errorf("SidePanel did not land as its modifier:\n%s", h)
	}
	if plain := string(FormFrame(FormFrameConfig{Side: []render.HTML{render.Text("s")}})); strings.Contains(plain, "side-panel") {
		t.Errorf("the plain rail shipped the panel modifier:\n%s", plain)
	}
	css := formFrameStyle.Entry().CSSFor(theme.Default())
	at := strings.Index(css, "@container")
	if at < 0 {
		t.Fatalf("no container query:\n%s", css)
	}
	before, inside := css[:at], css[at:]
	if !strings.Contains(before, `.fui-form-frame--side-panel .fui-form-frame__side {`) ||
		!strings.Contains(before, "var(--color-surface)") || !strings.Contains(before, "var(--color-border)") {
		t.Errorf("the panel rule does not draw a tokened surface:\n%s", css)
	}
	if !strings.Contains(inside, "position: sticky") || strings.Contains(before, "position: sticky") {
		t.Errorf("the panel sticks outside the side-by-side posture:\n%s", css)
	}
	if !strings.Contains(css, "max-inline-size: var(--ui-form-frame-main-max, 45rem)") {
		t.Errorf("the main column holds no readable measure:\n%s", css)
	}
}
