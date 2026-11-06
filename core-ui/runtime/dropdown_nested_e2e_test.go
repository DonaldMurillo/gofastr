package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestNestedDropdownKeepsAncestors(t *testing.T) {
	base := startPollServer(t, `<!doctype html><html><head><title>Nested menus</title></head><body>
<div data-cui-dropdown-wrap><button id="outer" data-cui-dropdown>Outer</button><div id="outer-panel" data-cui-dropdown-panel hidden>
  <div data-cui-dropdown-wrap><button id="middle" data-cui-dropdown>Middle</button><div id="middle-panel" data-cui-dropdown-panel hidden>
    <div data-cui-dropdown-wrap><button id="inner" data-cui-dropdown><span>Inner</span></button><div id="inner-panel" data-cui-dropdown-panel hidden><button id="inner-item">Pick</button></div></div>
    <div data-cui-dropdown-wrap><button id="sibling" data-cui-dropdown>Sibling</button><div id="sibling-panel" data-cui-dropdown-panel hidden><button id="sibling-item">Pick</button></div></div>
  </div></div>
</div></div>
<button id="outside">Outside</button><script src="/__gofastr/runtime.js"></script></body></html>`, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.Poll(`window.__gofastr?.loadedModules?.dropdown`, nil),
		chromedp.Click(`#outer`, chromedp.ByID),
		chromedp.Click(`#middle`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('inner').firstElementChild.click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`['outer','middle','inner'].map(id => document.getElementById(id).getAttribute('aria-expanded') + ':' + !document.getElementById(id+'-panel').hidden).join('|')`, &state)); err != nil {
		t.Fatal(err)
	}
	if state != "true:true|true:true|true:true" {
		t.Fatalf("nested opening hid its ancestors: %s", state)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#sibling`, chromedp.ByID),
		chromedp.Focus(`#sibling-item`, chromedp.ByID),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(`['outer','middle','inner','sibling'].map(id => document.getElementById(id+'-panel').hidden).join('|')+'|'+document.activeElement.id`, &state),
	); err != nil {
		t.Fatal(err)
	}
	if state != "false|false|true|true|sibling" {
		t.Fatalf("sibling opening or Escape dismissed an ancestor: %s", state)
	}
	// Focus on a nested trigger sits inside its ancestors' panels, not its
	// own: Escape still closes the dropdown that trigger owns.
	if err := chromedp.Run(ctx,
		chromedp.Click(`#sibling`, chromedp.ByID),
		chromedp.Focus(`#sibling`, chromedp.ByID),
		chromedp.KeyEvent("\x1b"),
		chromedp.Evaluate(`['outer','middle','sibling'].map(id => document.getElementById(id+'-panel').hidden).join('|')+'|'+document.activeElement.id`, &state),
	); err != nil {
		t.Fatal(err)
	}
	if state != "false|false|true|sibling" {
		t.Fatalf("Escape on a nested trigger closed the wrong dropdown: %s", state)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#outside`, chromedp.ByID),
		chromedp.Evaluate(`String(document.querySelectorAll('[data-cui-dropdown-open]').length)`, &state),
	); err != nil {
		t.Fatal(err)
	}
	if state != "0" {
		t.Fatalf("outside click left %s dropdowns open", state)
	}
}
