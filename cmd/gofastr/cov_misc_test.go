package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestAppSpecTypedBlockHelpersAreByteIdentical: the p/div emitters
// switched from render.Tag to html.Paragraph/html.Div on the claim the
// typed helpers produce the same bytes. Pin it at the exact call shapes
// emitted, for both the classless and the classed block: writeAttrs
// renders nil and the empty map identically, and buildAttrs only sets
// class when non-empty — if either drifts, a generated app's markup
// changes with no diff in the generator's own tests.
func TestAppSpecTypedBlockHelpersAreByteIdentical(t *testing.T) {
	for _, class := range []string{"", "lead"} {
		attrs := map[string]string(nil)
		if class != "" {
			attrs = map[string]string{"class": class}
		}
		oldP := string(render.Tag("p", attrs, render.Text("hi")))
		newP := string(html.Paragraph(html.TextConfig{Class: class}, render.Text("hi")))
		if oldP != newP {
			t.Errorf("p class=%q: render.Tag gives %q, html.Paragraph gives %q", class, oldP, newP)
		}
		oldD := string(render.Tag("div", attrs, render.Text("hi")))
		newD := string(html.Div(html.DivConfig{Class: class}, render.Text("hi")))
		if oldD != newD {
			t.Errorf("div class=%q: render.Tag gives %q, html.Div gives %q", class, oldD, newD)
		}
	}
}
