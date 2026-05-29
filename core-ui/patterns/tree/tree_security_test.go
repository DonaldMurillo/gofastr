package tree

import (
	"strings"
	"testing"
)

// TestHrefSchemeAllowlist asserts a leaf Node.Href carrying a
// script-executing or origin-ambiguous scheme never reaches the
// anchor href — the node degrades to a plain <span> label instead.
func TestHrefSchemeAllowlist(t *testing.T) {
	safe := []string{
		"/docs/page",
		"https://example.com/x",
		"#section",
		"mailto:a@b.com",
	}
	for _, h := range safe {
		out := string(Render(Config{
			ID:    "t",
			Label: "T",
			Nodes: []Node{{ID: "n", Label: "Leaf", Href: h}},
		}))
		if !strings.Contains(out, `href="`+h+`"`) {
			t.Errorf("safe href %q should render as a link\nout: %s", h, out)
		}
	}

	dangerous := []string{
		"javascript:alert(document.cookie)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.example.com/x",
		"java\tscript:alert(1)", // control byte smuggling
	}
	for _, h := range dangerous {
		out := string(Render(Config{
			ID:    "t",
			Label: "T",
			Nodes: []Node{{ID: "n", Label: "Leaf", Href: h}},
		}))
		if strings.Contains(out, "<a ") {
			t.Errorf("dangerous href %q rendered an anchor\nout: %s", h, out)
		}
		// The dangerous scheme must not survive inside any href attr.
		if strings.Contains(out, "javascript:") || strings.Contains(out, "vbscript:") || strings.Contains(out, "data:text/html") {
			t.Errorf("dangerous scheme survived for %q\nout: %s", h, out)
		}
	}
}
