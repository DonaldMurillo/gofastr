package html

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestHTMLBuildersDropJSSchemes pins the URL-scheme allow-list across every
// core-ui/html builder that emits a URL attribute.
//
// The property, "a URL attribute never carries a script-executing or
// origin-ambiguous scheme", was enforced in framework/ui (safeURL) and in
// core-ui/patterns/combobox (safePushHref) but NOT in core-ui/html, the
// layer both of those sit on top of and the layer core-ui/noderender
// renders its explicitly-untrusted IR through. The guard belongs at the
// bottom so every caller inherits it.
//
// Case shapes are the distinct threat classes, not variants: script
// execution (javascript:, case-folded, leading whitespace), document
// substitution (data:), origin ambiguity (protocol-relative), and header
// smuggling (encoded CRLF).
func TestHTMLBuildersDropJSSchemes(t *testing.T) {
	unsafe := []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"  javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"//evil.example/steal",
		"https://ok.example/%0d%0aX-Injected:1",
	}

	// Each surface renders the candidate into its URL attribute and returns
	// the markup. Anchors additionally accept mailto:/tel:; resource
	// surfaces (img, source, script, stylesheet) accept http(s) and
	// relative references only.
	surfaces := []struct {
		name   string
		anchor bool
		render func(u string) render.HTML
	}{
		{"Link.Href", true, func(u string) render.HTML { return Link(LinkConfig{Href: u, Text: "x"}) }},
		{"LinkHTML.Href", true, func(u string) render.HTML {
			return LinkHTML(LinkHTMLConfig{Href: u, Content: render.Text("x")})
		}},
		{"Form.Action", true, func(u string) render.HTML { return Form(FormConfig{Method: "post", Action: u}) }},
		{"Image.Src", false, func(u string) render.HTML { return Image(ImageConfig{Src: u, Alt: "x"}) }},
		{"Source.Src", false, func(u string) render.HTML { return Source(SourceConfig{Src: u, Type: "video/mp4"}) }},
		{"StyleSheet", false, func(u string) render.HTML { return StyleSheet(u) }},
		{"Script", false, func(u string) render.HTML { return Script(u) }},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			for _, u := range unsafe {
				got := string(s.render(u))
				if strings.Contains(got, "javascript") || strings.Contains(got, "data:text/html") ||
					strings.Contains(got, "//evil.example") || strings.Contains(got, "%0d") {
					t.Errorf("SECURITY: [xss] %s kept unsafe URL %q. Rendered: %s", s.name, u, got)
				}
			}

			// Legitimate URLs must survive, a guard that drops everything
			// is not a guard.
			for _, ok := range []string{"/relative/path", "https://ok.example/x", "#frag"} {
				got := string(s.render(ok))
				if !strings.Contains(got, ok) {
					t.Errorf("%s dropped the safe URL %q. Rendered: %s", s.name, ok, got)
				}
			}

			// mailto:/tel: are meaningful on an anchor and meaningless (so
			// refused) on a resource reference.
			got := string(s.render("mailto:a@b.example"))
			if s.anchor && !strings.Contains(got, "mailto:") {
				t.Errorf("%s dropped mailto: on an anchor surface. Rendered: %s", s.name, got)
			}
			if !s.anchor && strings.Contains(got, "mailto:") {
				t.Errorf("SECURITY: [xss] %s kept mailto: on a resource surface. Rendered: %s", s.name, got)
			}
		})
	}
}
