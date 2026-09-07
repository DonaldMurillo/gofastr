//go:build red

package widget

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// [CONTRACT-QUESTION red line FIRST] reach caveat: every in-tree caller today passes a
// Position constant (widget.New defaults BottomRight; preset.Builders mount the constant
// set), so this is a hardening invariant, not an exploitable path as wired. But
// escape_test.go documents the chrome contract as "the canonical 5-char escaper on the
// widget chrome attributes" and names the motivating class "a real attribute-breakout XSS
// class"; Position is interpolated raw, so the documented invariant is broken for exactly
// one chrome field. Delete this test only if the decided contract is instead "Position is
// validated against the constant set" (it is a `type Position string` today — nothing
// constrains it).
// Property: every value the widget chrome interpolates into an attribute is escaped;
// Definition.Position is the one field that reaches defaultSkeleton's HTML unescaped.
// Surfaces: core-ui/widget/server.go::defaultSkeleton :485 — `<div class="fui-widget
// fui-pos-` + string(def.Position) + `"` — while Name (:485), Role (:487), LabelledBy
// (:496), DescribedBy (:499), and slot names (:541) all go through render.Escape.
// Finding: Definition{Position: `x" onmouseover="alert(1)`} emits the raw attribute
// breakout into the page chrome (verified 2026-09-06).
// Fix direction: render.Escape(def.Position) like every other chrome field, or validate
// Position against the constant set at Definition/Build time.

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

	// Hostile Position: must not break out of the class attribute.
	hostile := `x" onmouseover="alert(1)`
	out := string(defaultSkeleton(Definition{Name: "w", Position: Position(hostile), Role: "r"}, map[string]render.HTML{"body": render.Text("x")}))
	if strings.Contains(out, `onmouseover=`) || strings.Contains(out, hostile) {
		t.Errorf("SECURITY: [widget-position-attr-breakout] hostile Position interpolated raw into the widget chrome: defaultSkeleton emits the Definition.Position string unescaped, so `x\" onmouseover=\"alert(1)` breaks out of the class attribute — every other chrome field goes through render.Escape:\n%s", out)
	}
}
