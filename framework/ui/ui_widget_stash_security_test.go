package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: an inline <script type="application/json"> stash that parks
// server-rendered HTML for later hydration must not be terminable (or
// comment-openable) by the parked content itself. The stash idiom is
// json.Marshal (which escapes <, >, & to \u003c…) plus the </ → <\/
// rewrite; the attack shape is panel/slide content that contains
// </script><script>… and tries to close the data script early and open
// a live one.
//
// Surfaces: every framework/ui emitter of the idiom — Tabs.VacateHidden
// (hidden tab panels) and Carousel.VirtualScroll (deferred slides).
// Content in both is page content, which on real screens includes
// user/record-derived fragments.
func TestInlineJSONStashResistsScriptClose(t *testing.T) {
	evil := `</script><script>alert(1)</script><!--<script>`

	tabs := string(ui.Tabs(ui.TabsConfig{
		SignalName:   "t",
		VacateHidden: true,
		Tabs: []ui.TabItem{
			{Label: "A", Content: render.Text("alpha")},
			{Label: "B", Content: render.HTML(evil)}, // inactive → parked in stash
		},
	}))
	carousel := string(ui.Carousel(ui.CarouselConfig{
		Label:         "L",
		VirtualScroll: true,
		Slides: []ui.CarouselSlide{
			{Content: render.Text("s1")}, {Content: render.Text("s2")},
			{Content: render.Text("s3")}, {Content: render.Text("s4")},
			{Content: render.Text("s5")}, {Content: render.HTML(evil)}, // beyond window → deferred
		},
	}))

	for name, h := range map[string]string{"tabs-vacate-stash": tabs, "carousel-deferred-manifest": carousel} {
		t.Run(name, func(t *testing.T) {
			// The stash script region is the only legitimate </script> in
			// play; the payload's own one must never appear raw.
			if strings.Contains(h, `</script><script>`) {
				t.Errorf("SECURITY: %s: payload closed the JSON stash and opened a live script:\n%s", name, h)
			}
			// json.Marshal's HTML escaping neutralises every < in the
			// payload; the </ → <\/ rewrite is the second layer.
			if !strings.Contains(h, `\u003c`) && !strings.Contains(h, `<\/`) {
				t.Errorf("%s: expected stash content to be JSON-escaped, got:\n%s", name, h)
			}
			if strings.Contains(h, `<!--`) {
				t.Errorf("SECURITY: %s: raw <!-- reached output; script-data-escaped state tricks:\n%s", name, h)
			}
		})
	}
}
