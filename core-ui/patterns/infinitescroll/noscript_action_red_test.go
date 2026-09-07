//go:build red

package infinitescroll

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T2). Pinned sibling: TestFormActionSinksRejectUnsafeURL
// (framework/ui/ui_link_form_security_test.go) — the <form action> sink family
// allow-lists URL schemes across SearchInput/FilterToolbar/SignOut/StepWizard,
// and its header documents the exact bug class: render.Escape is HTML-escaping
// and scheme-blind, so a hand-rolled form tag must gate its action like every
// other sink.
// Property: every <form action> sink allow-lists schemes — the noscript
// fallback form's action (built with render.Escape(cfg.RPCPath) into a raw
// string, bypassing html.Form's setURLAttr(urlsafe.Anchor) guard) must not
// carry a live javascript:/vbscript:/data: scheme or a protocol-relative host.
// Surfaces: core-ui/patterns/infinitescroll/infinitescroll.go::Render :74-78 —
// the noscript form action; probed: action="javascript:alert(1)" verbatim.
// Finding: a hostile RPCPath renders as a live form action today; under
// noscript the browser executes the javascript: URL on submit.
// Fix direction: run cfg.RPCPath through urlsafe.Clean(cfg.RPCPath,
// urlsafe.Anchor) (or the equivalent scheme allow-list) before interpolating
// it into the action attribute.

// redNoscriptAction extracts the noscript form's action attribute value from
// the rendered output (the only action= in the component's markup).
func redNoscriptAction(out string) string {
	const needle = `action="`
	i := strings.Index(out, needle)
	if i < 0 {
		return ""
	}
	rest := out[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestNoScriptActionRedSchemeGuarded: hostile schemes in Config.RPCPath must
// not reach the noscript fallback form's action attribute.
func TestNoScriptActionRedSchemeGuarded(t *testing.T) {
	attacks := []struct{ name, path string }{
		{"javascript", "javascript:alert(1)"},
		{"vbscript", "vbscript:x"},
		{"data", "data:text/html,x"},
		{"protocol-relative", "//evil.example/p"},
	}
	for _, a := range attacks {
		t.Run(a.name, func(t *testing.T) {
			out := string(Render(Config{
				RPCPath: a.path,
				Items:   []render.HTML{render.Text("item one")},
				Cursor:  "cur-7",
			}))
			action := redNoscriptAction(out)
			if action == "" {
				t.Fatalf("setup broken: no form action rendered in output:\n%s", out)
			}
			low := strings.ToLower(action)
			if strings.HasPrefix(low, "javascript:") || strings.HasPrefix(low, "vbscript:") ||
				strings.HasPrefix(low, "data:") || strings.HasPrefix(action, "//") {
				t.Errorf("SECURITY: [infinitescroll-noscript-action] noscript fallback form action = %q "+
					"(from RPCPath %q): the hand-rolled form tag bypasses html.Form's setURLAttr(urlsafe.Anchor) "+
					"guard that every other form-action sink in the family enforces, so a hostile RPCPath "+
					"renders as a live form action — under noscript the browser executes it on submit",
					action, a.path)
			}
		})
	}

	// Controls: a valid relative RPCPath round-trips and the cursor field
	// survives — the guard is a scheme allow-list, not a blanket reject.
	out := string(Render(Config{
		RPCPath: "/islands/items",
		Items:   []render.HTML{render.Text("item one")},
		Cursor:  "cur-7",
	}))
	if !strings.Contains(out, `action="/islands/items"`) {
		t.Errorf("SECURITY: [infinitescroll-noscript-action] control: valid relative action dropped:\n%s", out)
	}
	if !strings.Contains(out, `name="cursor" value="cur-7"`) {
		t.Errorf("SECURITY: [infinitescroll-noscript-action] control: cursor field lost from the noscript form:\n%s", out)
	}
}
