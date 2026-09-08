package widget

// Pins: every value the widget chrome interpolates into an attribute is
// escaped — Definition.Position included (contract answer, round 5:
// render.Escape(def.Position), like every other chrome field). Escaping is
// the decided contract rather than constant-set validation, so the pinned
// breakout shape is a LITERAL quote surviving into the attribute (which
// would open a new attribute); the escaped payload itself is inert class
// text. (2026-09-06 adversarial pass, round 5.)
// Property: every value the widget chrome interpolates into an attribute is escaped;
// Definition.Position is the one field that reaches defaultSkeleton's HTML unescaped.
// Surfaces: core-ui/widget/server.go::defaultSkeleton — `<div class="fui-widget
// fui-pos-` + string(def.Position) + `"` — while Name, Role, LabelledBy,
// DescribedBy, and slot names all go through render.Escape.

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestChromeRedPositionEscaped(t *testing.T) {
	// Control: a real Position round-trips as the fui-pos-<pos> class.
	ctl := string(defaultSkeleton(Definition{Name: "w", Position: BottomRight}, map[string]render.HTML{"body": render.Text("x")}))
	if !strings.Contains(ctl, `fui-pos-bottom-right"`) {
		t.Errorf("control broken: BottomRight no longer renders as fui-pos-bottom-right:\n%s", ctl)
	}

	// Hostile Position: must not break out of the class attribute. Under the
	// escape contract the payload may appear ESCAPED (inert class text), so
	// the pinned shape is the breakout itself: a literal quote opening a new
	// attribute, or the raw hostile string verbatim.
	hostile := `x" onmouseover="alert(1)`
	out := string(defaultSkeleton(Definition{Name: "w", Position: Position(hostile), Role: "r"}, map[string]render.HTML{"body": render.Text("x")}))
	if strings.Contains(out, `onmouseover="`) || strings.Contains(out, hostile) {
		t.Errorf("SECURITY: [widget-position-attr-breakout] hostile Position interpolated raw into the widget chrome: defaultSkeleton emits the Definition.Position string unescaped, so `x\" onmouseover=\"alert(1)` breaks out of the class attribute — every other chrome field goes through render.Escape:\n%s", out)
	}
}
