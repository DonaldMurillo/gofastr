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

func TestTrapSkipsUnfocusable(t *testing.T) {
	nested := headless.Disclosure(headless.DisclosureProps{
		ID: "nested", Summary: render.Text("Nested controls"), Open: true,
		Parts: headless.Parts{Attrs: headless.PartAttrs{headless.PartSummary: {"tabindex": "-1"}}},
		Content: render.Join(
			render.Tag("input", map[string]string{"id": "nested-first", "aria-label": "Nested first"}),
			render.Tag("button", map[string]string{"id": "nested-last"}, render.Text("Nested last")),
		),
	}, nil)
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		t.Run(surface, func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", surface, "")
			ctx := comboboxLinkPage(t, server, "ssr", surface)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`
				window.trapRoot = document.getElementById('links').closest('[data-cui-widget],[data-hui-disclosure],[data-hui-pane]') || document.getElementById('cui-intercept').lastElementChild;
				trapRoot.querySelectorAll(__gofastr._focusSel).forEach(control => control.tabIndex = -1);
				const first = document.createElement('input'); first.id = 'outer-first'; trapRoot.append(first);
				const last = document.createElement('button'); last.id = 'outer-last'; last.textContent = 'Last'; trapRoot.append(last);
				for (const position of ['beforebegin', 'afterend']) {
					const outside = document.createElement('button'); outside.id = 'outside-' + position;
					trapRoot.insertAdjacentElement(position, outside);
				}
				document.addEventListener('keydown', event => {
					if (event.key === 'Tab') window.trapTabTrusted = event.isTrusted;
				}, true);
			`, nil)); err != nil {
				t.Fatal(err)
			}
			// The native pass uses Element.checkVisibility; the fallback
			// pass deletes it, as WebKit before Safari 17.4 has none.
			for _, path := range []string{"native", "fallback"} {
				if path == "fallback" {
					if err := chromedp.Run(ctx, chromedp.Evaluate(`delete Element.prototype.checkVisibility`, nil)); err != nil {
						t.Fatal(err)
					}
				}
				for _, mode := range []string{"closed", "closed-summary", "visibility-hidden", "content-visibility-hidden", "inert", "hidden", "display-none", "closed-css-visible", "visible", "disabled-fieldset", "disabled-tabindex"} {
					// ::details-content arrived after checkVisibility in every
					// engine, so no browser on the fallback path can show it.
					if path == "fallback" && mode == "closed-css-visible" {
						continue
					}
					for _, position := range []string{"first", "last"} {
						t.Run(path+"/"+mode+"/"+position, func(t *testing.T) {
							if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`{
							document.getElementById('nested')?.remove(); document.getElementById('nested-style')?.remove();
							trapRoot.insertAdjacentHTML(%q === 'first' ? 'afterbegin' : 'beforeend', %q);
							const nested = document.getElementById('nested'), panel = nested.querySelector('summary').nextElementSibling;
							const mode = %q;
							if (mode.startsWith('closed')) nested.open = false;
							if (mode === 'closed-summary') { const summary = nested.querySelector('summary'); summary.removeAttribute('tabindex'); summary.id = 'nested-summary'; }
							if (mode === 'visibility-hidden') panel.style.visibility = 'hidden';
							if (mode === 'content-visibility-hidden') panel.style.contentVisibility = 'hidden';
							if (mode === 'inert') panel.inert = true;
							if (mode === 'hidden') panel.hidden = true;
							if (mode === 'display-none') panel.style.display = 'none';
							if (mode === 'closed-css-visible') {
								const style = document.createElement('style'); style.id = 'nested-style';
								style.textContent = '#nested::details-content { content-visibility:visible !important; }'; document.head.append(style);
							}
							if (mode === 'disabled-fieldset') {
								const fieldset = document.createElement('fieldset'); fieldset.disabled = true;
								fieldset.append(...panel.children); panel.append(fieldset);
							}
							if (mode === 'disabled-tabindex') panel.querySelectorAll('input,button').forEach(control => { control.disabled = true; control.tabIndex = 0; });
						}`, position, nested, mode), nil)); err != nil {
								t.Fatal(err)
							}
							first, last := "outer-first", "outer-last"
							if mode == "visible" || mode == "closed-css-visible" {
								if position == "first" {
									first = "nested-first"
								} else {
									last = "nested-last"
								}
							}
							if mode == "closed-summary" {
								if position == "first" {
									first = "nested-summary"
								} else {
									last = "nested-summary"
								}
							}
							for _, reverse := range []bool{true, false} {
								start, want, modifiers := last, first, input.ModifierNone
								if reverse {
									start, want, modifiers = first, last, input.ModifierShift
								}
								var state struct {
									Focused string
									Trusted bool
								}
								if err := chromedp.Run(ctx,
									chromedp.Evaluate(fmt.Sprintf(`document.getElementById(%q).focus()`, start), nil),
									chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(modifiers)),
									chromedp.Evaluate(`({Focused: document.activeElement.id || document.activeElement.tagName, Trusted: trapTabTrusted})`, &state),
								); err != nil {
									t.Fatal(err)
								}
								if state.Focused != want || !state.Trusted {
									t.Errorf("Tab from %s reverse=%t: %+v, want trusted focus on %s", start, reverse, state, want)
								}
							}
						})
					}
				}
			}
		})
	}
}
