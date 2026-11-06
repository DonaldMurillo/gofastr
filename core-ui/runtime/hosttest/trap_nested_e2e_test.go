package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

func TestTrapKeepsNestedTabOrder(t *testing.T) {
	nested := headless.Disclosure(headless.DisclosureProps{
		ID: "nested", Summary: render.Text("Nested controls"), Open: true,
		Parts: headless.Parts{Attrs: headless.PartAttrs{headless.PartSummary: {"tabindex": "-1"}}},
		Content: render.Join(
			render.Tag("input", map[string]string{"id": "nested-first", "aria-label": "First"}),
			render.Tag("input", map[string]string{"id": "nested-negative", "aria-label": "Programmatic", "tabindex": " -4 "}),
			render.Tag("button", map[string]string{"id": "nested-second"}, render.Text("Second")),
		),
	}, nil)
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		for _, position := range []string{"middle", "last", "only"} {
			t.Run(surface+"/"+position, func(t *testing.T) {
				server := comboboxLinkSite(t, "ssr", surface, "")
				ctx := comboboxLinkPage(t, server, "ssr", surface)
				if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
					window.trapRoot = document.getElementById('links').closest('[data-cui-widget],[data-hui-disclosure],[data-hui-pane]') || document.getElementById('cui-intercept').lastElementChild;
					if (%q === 'only') trapRoot.querySelectorAll(__gofastr._focusSel).forEach(control => control.tabIndex = -1);
					trapRoot.insertAdjacentHTML('beforeend', %q);
					if (%q === 'middle') {
						const tail = document.createElement('button'); tail.id = 'outer-last'; tail.textContent = 'Last'; trapRoot.append(tail);
					}
				`, position, nested, position), nil)); err != nil {
					t.Fatal(err)
				}
				previous, next := "links", "links"
				if position == "middle" {
					next = "outer-last"
				} else if position == "only" {
					previous, next = "nested-second", "nested-first"
				}
				for _, step := range []struct {
					start   string
					reverse bool
					want    string
				}{
					{start: "nested-first", want: "nested-second"},
					{start: "nested-second", reverse: true, want: "nested-first"},
					{start: "nested-negative", want: "nested-second"},
					{start: "nested-negative", reverse: true, want: "nested-first"},
					{start: "nested-first", reverse: true, want: previous},
					{start: "nested-second", want: next},
				} {
					modifiers := input.ModifierNone
					if step.reverse {
						modifiers = input.ModifierShift
					}
					var focused string
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(fmt.Sprintf(`document.getElementById(%q).focus()`, step.start), nil),
						chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(modifiers)),
						chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused),
					); err != nil {
						t.Fatal(err)
					}
					if focused != step.want {
						t.Errorf("Tab from %s reverse=%t focused %q, want %s", step.start, step.reverse, focused, step.want)
					}
				}
			})
		}
	}
}
