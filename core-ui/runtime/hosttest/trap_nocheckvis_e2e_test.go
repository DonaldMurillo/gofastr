package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// TestTrapWorksWithoutCheckVisibility runs every focus trap in a page with
// no Element.checkVisibility (WebKit before Safari 17.4, so an older
// macOS desktop shell): Tab and Shift+Tab still wrap, a hidden control
// is still not a stop, and a position:fixed control (whose offsetParent
// is null) still is.
func TestTrapWorksWithoutCheckVisibility(t *testing.T) {
	for _, surface := range []string{"widget", "intercept", "disclosure", "panehost"} {
		t.Run(surface, func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", surface, "")
			ctx := comboboxLinkPage(t, server, "ssr", surface)
			if err := chromedp.Run(ctx, chromedp.Evaluate(`
				delete Element.prototype.checkVisibility;
				window.trapRoot = document.getElementById('links').closest('[data-cui-widget],[data-hui-disclosure],[data-hui-pane]') || document.getElementById('cui-intercept').lastElementChild;
				trapRoot.querySelectorAll(__gofastr._focusSel).forEach(control => control.tabIndex = -1);
				const first = document.createElement('input'); first.id = 'outer-first'; trapRoot.append(first);
				const last = document.createElement('button'); last.id = 'outer-last'; last.textContent = 'Last'; last.style.cssText = 'position:fixed;bottom:0;right:0'; trapRoot.append(last);
				const hidden = document.createElement('button'); hidden.id = 'hidden-last'; hidden.hidden = true; trapRoot.append(hidden);
				const outside = document.createElement('button'); outside.id = 'outside-afterend'; trapRoot.insertAdjacentElement('afterend', outside);
			`, nil)); err != nil {
				t.Fatal(err)
			}
			for _, reverse := range []bool{false, true} {
				start, want, modifiers := "outer-last", "outer-first", input.ModifierNone
				if reverse {
					start, want, modifiers = "outer-first", "outer-last", input.ModifierShift
				}
				var focused string
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(fmt.Sprintf(`document.getElementById(%q).focus()`, start), nil),
					chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(modifiers)),
					chromedp.Evaluate(`document.activeElement.id || document.activeElement.tagName`, &focused),
				); err != nil {
					t.Fatal(err)
				}
				if focused != want {
					t.Errorf("Tab from %s reverse=%t focused %s, want %s", start, reverse, focused, want)
				}
			}
		})
	}
}
