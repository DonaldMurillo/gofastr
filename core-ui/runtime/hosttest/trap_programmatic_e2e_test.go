package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestTrapKeepsProgrammaticFocus(t *testing.T) {
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		t.Run(surface, func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", surface, `<button tabindex="-1" autofocus>Programmatic focus</button>`)
			ctx := comboboxLinkPage(t, server, "ssr", surface)
			if surface == "widget" {
				var initial string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`trapInitialFocus`, &initial)); err != nil {
					t.Fatal(err)
				}
				if initial != "BUTTON" {
					t.Errorf("autofocus target was not selected on open: %q", initial)
				}
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`
				window.trapRoot = document.getElementById('links').closest('[data-cui-widget],[data-hui-disclosure],[data-hui-pane]') || document.getElementById('cui-intercept').lastElementChild;
				window.trapTarget = trapRoot.querySelector('[autofocus]');
				window.trapFirst = document.createElement('input'); trapFirst.id = 'trap-first';
				window.trapLast = document.createElement('input'); trapLast.id = 'trap-last';
				trapRoot.prepend(trapFirst); trapRoot.append(trapLast);
				for (const position of ['beforebegin', 'afterend']) {
					const outside = document.createElement('button'); outside.id = 'outside-' + position;
					trapRoot.insertAdjacentElement(position, outside);
				}
			`, nil)); err != nil {
				t.Fatal(err)
			}
			for _, tabindex := range []string{"-1", "-7", " -12 "} {
				for _, position := range []string{"before", "middle", "after"} {
					for _, reverse := range []bool{true, false} {
						var focused string
						modifiers := input.ModifierNone
						if reverse {
							modifiers = input.ModifierShift
						}
						if err := chromedp.Run(ctx,
							chromedp.Evaluate(fmt.Sprintf(`
								trapTarget.setAttribute('tabindex', %q);
								if (%q === 'before') trapFirst.before(trapTarget);
								else if (%q === 'after') trapLast.after(trapTarget);
								else trapFirst.after(trapTarget);
								trapTarget.focus();
							`, tabindex, position, position), nil),
							chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(modifiers)),
							chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused),
						); err != nil {
							t.Fatal(err)
						}
						want := "trap-first"
						if reverse && position != "middle" {
							want = "trap-last"
						} else if !reverse && position == "middle" {
							want = "links"
						}
						if focused != want {
							t.Errorf("tabindex=%q position=%s reverse=%t focused %q, want %s", tabindex, position, reverse, focused, want)
						}
					}
				}
			}
			for _, reverse := range []bool{true, false} {
				modifiers := input.ModifierNone
				if reverse {
					modifiers = input.ModifierShift
				}
				var retained bool
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`trapRoot.querySelectorAll(__gofastr._focusSel).forEach(control => control.tabIndex = -1); trapTarget.focus()`, nil),
					chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(modifiers)),
					chromedp.Evaluate(`document.activeElement === trapTarget`, &retained),
				); err != nil {
					t.Fatal(err)
				}
				if !retained {
					t.Errorf("empty trap lost focus, reverse=%t", reverse)
				}
			}
		})
	}
}
