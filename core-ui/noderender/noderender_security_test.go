package noderender

import (
	"strings"
	"testing"
)

// TestIRDropsScriptAndURLGadgets pins the attribute policy for the node
// IR. node.go documents the IR as agent-authored and untrusted, and drops
// a "raw" kind for exactly that reason — but the attribute pass-through
// was a DENY-list of three names (style, srcdoc, on*), so everything the
// browser runtime treats as privileged sailed through: data-behavior
// (an unvalidated <script src> sink), data-widget / data-component /
// data-bind, and any data-fui-* the runtime acts on. URL-valued
// attributes had no scheme guard at all.
//
// The property is "an untrusted IR emits only inert presentational
// attributes". Case shapes: the script-loading gadget, the runtime
// gadgets, a case-folded event handler (the old check was
// case-sensitive), and URL attributes carrying javascript:/data:.
func TestIRDropsScriptAndURLGadgets(t *testing.T) {
	gadgets := []struct {
		name  string
		props map[string]any
		bad   string
	}{
		{"data-behavior script sink", map[string]any{"data-behavior": "https://evil.example/x.js"}, "data-behavior"},
		{"data-widget", map[string]any{"data-widget": "admin"}, "data-widget"},
		{"data-component", map[string]any{"data-component": "admin"}, "data-component"},
		{"data-bind", map[string]any{"data-bind": "secret"}, "data-bind"},
		{"data-fui-poll-src", map[string]any{"data-fui-poll-src": "https://evil.example/p"}, "data-fui-poll-src"},
		{"data-fui-rpc", map[string]any{"data-fui-rpc": "https://evil.example/r"}, "data-fui-rpc"},
		{"data-fui-signal-set", map[string]any{"data-fui-signal-set": "__proto__:x"}, "data-fui-signal-set"},
		{"style", map[string]any{"style": "background:url(x)"}, "style"},
		{"srcdoc", map[string]any{"srcdoc": "<script>alert(1)</script>"}, "srcdoc"},
		{"case-folded handler", map[string]any{"OnClick": "alert(1)"}, "alert(1)"},
		{"uppercase handler", map[string]any{"ONMOUSEOVER": "alert(1)"}, "alert(1)"},
	}
	for _, g := range gadgets {
		t.Run(g.name, func(t *testing.T) {
			got := string(RenderKind("div", g.props, nil))
			if strings.Contains(got, g.bad) {
				t.Errorf("SECURITY: [xss] untrusted IR emitted %s. Rendered: %s", g.bad, got)
			}
		})
	}

	urls := []struct {
		name  string
		kind  string
		props map[string]any
	}{
		{"anchor javascript:", "a", map[string]any{"href": "javascript:alert(1)", "text": "x"}},
		{"img data:text/html", "img", map[string]any{"src": "data:text/html,<script>alert(1)</script>", "alt": "x"}},
		{"form javascript:", "form", map[string]any{"action": "javascript:alert(1)", "method": "post"}},
		{"anchor protocol-relative", "a", map[string]any{"href": "//evil.example/x", "text": "x"}},
	}
	for _, u := range urls {
		t.Run(u.name, func(t *testing.T) {
			got := string(RenderKind(u.kind, u.props, nil))
			if strings.Contains(got, "javascript:") || strings.Contains(got, "data:text/html") || strings.Contains(got, "//evil.example") {
				t.Errorf("SECURITY: [xss] untrusted IR emitted an unsafe URL. Rendered: %s", got)
			}
		})
	}

	// The allow-list must still let presentational and accessibility
	// attributes through — a renderer that drops everything is useless.
	got := string(RenderKind("div", map[string]any{
		"id": "card", "class": "row", "role": "region",
		"aria-label": "Card", "data-testid": "card", "title": "hi",
		// Inert host markers: read only by the host's own generated code
		// (the blueprint's screens, kiln's tool delegator), never by the
		// runtime. Blocking these would break every generator that renders
		// through this IR without closing any of the gadgets above.
		"data-field": "title", "data-entity-list-body": "posts",
		"data-action": "save", "data-kiln-tool": "chat",
	}, nil))
	for _, want := range []string{`id="card"`, `class="row"`, `role="region"`, `aria-label="Card"`, `data-testid="card"`, `title="hi"`,
		`data-field="title"`, `data-entity-list-body="posts"`, `data-action="save"`, `data-kiln-tool="chat"`} {
		if !strings.Contains(got, want) {
			t.Errorf("allow-list dropped legitimate attribute %s. Rendered: %s", want, got)
		}
	}
}
