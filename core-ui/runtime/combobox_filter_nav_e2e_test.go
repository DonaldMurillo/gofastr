package runtime

import (
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// comboboxFilterPage mirrors the SSR shape of a static-options combobox:
// a [role=combobox] input paired via aria-controls with a listbox that
// carries data-fui-static-options and renders every option inline. The
// listbox starts hidden — closed-by-default since 8509d370 — which makes
// the keyboard path the common one. Inlined for the same reason as
// tagInputPage: framework/ui transitively imports this package.
const comboboxFilterPage = `
<div>
  <input type="text" id="pick" role="combobox" aria-controls="pick-list"
         aria-expanded="false" aria-haspopup="listbox" aria-autocomplete="list"
         autocomplete="off" placeholder="Pick a word">
  <ul id="pick-list" role="listbox" data-fui-static-options hidden>
    <li role="option" id="opt-alpha" data-value="alpha">Alpha</li>
    <li role="option" id="opt-bravo" data-value="bravo">Bravo</li>
    <li role="option" id="opt-charlie" data-value="charlie">Charlie</li>
    <li role="option" id="opt-delta" data-value="delta">Delta</li>
  </ul>
</div>`

// TestComboboxKeyboardNavSkipsHiddenOpts pins that keyboard navigation
// only walks options the static filter left visible (#302). Type a query
// that leaves one option on screen, press ArrowDown twice: the highlight
// must wrap back to the visible option, never land on a hidden row, and
// Enter must pick the visible option — not one the user filtered away.
func TestComboboxKeyboardNavSkipsHiddenOpts(t *testing.T) {
	g := startGadgetServer(t, `[]`, comboboxFilterPage)
	ctx := newSeedBrowserCtx(t)

	// pressKey dispatches a synthetic keydown on the combobox input,
	// the same pattern taginput_e2e_test.go uses; the module listens
	// at document level and branches on e.key.
	pressKey := func(key string) chromedp.Action {
		return chromedp.Evaluate(`(function(){
			var el = document.getElementById('pick');
			el.dispatchEvent(new KeyboardEvent('keydown', { key: `+strconv.Quote(key)+`, bubbles: true, cancelable: true }));
		})()`, nil)
	}

	var afterFilterActive, afterDownActive, afterDownHidden string
	var afterEnterValue, afterEnterExpanded string

	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#pick`, chromedp.ByID),
		// The combobox module is demand-fetched off the [role=combobox]
		// marker. Poll for its self-registration instead of sleeping:
		// keys dispatched before the listener exists are silently
		// dropped and read as an intermittent failure.
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.combobox)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),

		// Type "cha": Alpha/Bravo/Delta are hidden, only Charlie matches.
		chromedp.Evaluate(`(function(){
			var el = document.getElementById('pick');
			el.focus();
			el.value = 'cha';
			el.dispatchEvent(new Event('input', { bubbles: true }));
		})()`, nil),
		chromedp.Poll(`!document.getElementById('opt-charlie').hidden && document.getElementById('opt-alpha').hidden`, nil,
			chromedp.WithPollingTimeout(5*time.Second), chromedp.WithPollingInterval(25*time.Millisecond)),
		chromedp.Evaluate(`document.getElementById('pick').getAttribute('aria-activedescendant')`, &afterFilterActive),

		// ArrowDown twice with one visible option: both presses must
		// wrap back to Charlie. Walking hidden rows moves
		// aria-activedescendant onto an invisible option instead.
		pressKey("ArrowDown"),
		pressKey("ArrowDown"),
		chromedp.Evaluate(`document.getElementById('pick').getAttribute('aria-activedescendant')`, &afterDownActive),
		chromedp.Evaluate(`String(document.getElementById('opt-charlie').hidden)`, &afterDownHidden),

		// Enter picks the highlighted option: Charlie.
		pressKey("Enter"),
		chromedp.Evaluate(`document.getElementById('pick').value`, &afterEnterValue),
		chromedp.Evaluate(`document.getElementById('pick').getAttribute('aria-expanded')`, &afterEnterExpanded),
	); err != nil {
		t.Fatal(err)
	}

	if afterFilterActive != "opt-charlie" {
		t.Errorf("filter: aria-activedescendant = %q, want \"opt-charlie\"", afterFilterActive)
	}
	if afterDownActive != "opt-charlie" {
		t.Errorf("ArrowDown x2: aria-activedescendant = %q, want \"opt-charlie\" (keyboard nav walked a hidden filtered-out row)", afterDownActive)
	}
	if afterDownHidden != "false" {
		t.Errorf("ArrowDown x2: highlighted option hidden = %s, want false", afterDownHidden)
	}
	if afterEnterValue != "charlie" {
		t.Errorf("Enter: input value = %q, want \"charlie\" (Enter selected a filtered-away option)", afterEnterValue)
	}
	if afterEnterExpanded != "false" {
		t.Errorf("Enter: aria-expanded = %q, want \"false\" (listbox must close on pick)", afterEnterExpanded)
	}
}
