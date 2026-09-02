package component

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"regexp"
	"strings"
	"testing"
)

// panicComponent panics with a caller-supplied message during Render.
type panicComponent struct{ msg string }

func (p panicComponent) Render() render.HTML { panic(p.msg) }

// TestSafeRenderEscapesPanicMessage asserts the fallback error UI
// HTML-escapes attacker-influenced panic text so it cannot inject markup.
func TestSafeRenderEscapesPanicMessage(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		raw    string // substring that must NOT appear verbatim (would be live markup)
		escErr string // escaped form that proves the message survived as text
	}{
		{
			name:   "img onerror",
			msg:    `<img src=x onerror=alert(1)>`,
			raw:    `<img src=x onerror=alert(1)>`,
			escErr: `&lt;img src=x onerror=alert(1)&gt;`,
		},
		{
			name:   "script tag",
			msg:    `</strong><script>alert(1)</script>`,
			raw:    `<script>alert(1)</script>`,
			escErr: `&lt;script&gt;`,
		},
		{
			name:   "attribute breakout",
			msg:    `"><svg/onload=alert(1)>`,
			raw:    `<svg/onload=alert(1)>`,
			escErr: `&lt;svg/onload=alert(1)&gt;`,
		},
		{
			name:   "benign message renders",
			msg:    `oh no`,
			raw:    "",
			escErr: `oh no`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html, err := SafeRender(panicComponent{msg: tc.msg})
			if err == nil {
				t.Fatal("expected an error from the panic")
			}
			out := string(html)
			if tc.raw != "" && strings.Contains(out, tc.raw) {
				t.Errorf("fallback UI contains live markup %q:\n%s", tc.raw, out)
			}
			if !strings.Contains(out, tc.escErr) {
				t.Errorf("fallback UI missing escaped message %q:\n%s", tc.escErr, out)
			}
		})
	}
}

// interactiveWidget is a component with one registered action so
// NewWidget extracts a non-empty ActionRegistry and Render emits the
// data-behavior hydration URL.
type interactiveWidget struct{}

func (interactiveWidget) Render() render.HTML { return render.Text("x") }
func (interactiveWidget) Actions()            { On("click", func(*ComponentContext) {}) }

// TestWidgetBehaviorURLMatchesRuntimeGate pins the shape contract the
// runtime documents at its data-behavior sink (runtime.js): only
// `/__gofastr/widget/[A-Za-z0-9_-]+.js` is honoured, because "any other
// value turns attribute injection into script execution". The gate is
// enforced in the BROWSER, so the emitter here must never produce a
// value the gate rejects: a widget ID outside that alphabet silently
// drops the behavior script (the widget never hydrates), and the
// tempting "fix" of loosening the runtime regex is exactly the
// script-injection hole the gate exists to prevent.
//
// Surface: Widget.Render's data-behavior emission, for IDs a host app
// might build dynamically (slugs, DB names, request-derived labels).
func TestWidgetBehaviorURLMatchesRuntimeGate(t *testing.T) {
	shape := regexp.MustCompile(`^/__gofastr/widget/[A-Za-z0-9_-]+\.js$`)
	ids := []string{
		"user-edit", // happy path
		"a b",       // space: slug built from a display name
		"inv/2024",  // slash: ID built from a path segment
		"café",      // non-ASCII
		`quote"`,    // quote breakout attempt
		"..%2eevil", // traversal-ish
		"tab\tid",   // control byte
	}
	for _, id := range ids {
		w := NewWidget(id, interactiveWidget{})
		out := string(w.Render())
		i := strings.Index(out, `data-behavior="`)
		if i < 0 {
			t.Fatalf("widget %q emitted no data-behavior — the harness component lost its actions", id)
		}
		val := out[i+len(`data-behavior="`):]
		if j := strings.Index(val, `"`); j >= 0 {
			val = val[:j]
		}
		val = strings.ReplaceAll(val, "&quot;", `"`)
		if !shape.MatchString(val) {
			t.Errorf("SECURITY: [widget-behavior] NewWidget(%q) emitted data-behavior=%q — outside the runtime's [A-Za-z0-9_-] shape the browser gate silently drops hydration; NewWidget must reject or slugify such IDs", id, val)
		}
	}
}
