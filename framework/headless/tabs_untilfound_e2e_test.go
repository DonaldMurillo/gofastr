package headless

// Browser coverage for the panels' hidden="until-found": the module
// keeps hidden and the panel tabindex in step with the selection, and a
// panel the browser reveals (here through a fragment link, the same
// beforematch path find-in-page takes) selects its tab.

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestE2E_TabsPanelsFollowSelection(t *testing.T) {
	strip := Tabs(TabsProps{Name: "pf", Tabs: []Tab{
		{Label: "General", Panel: render.Text("General panel")},
		{Label: "Secrets", Panel: render.Text("Secrets panel")},
		{Label: "Locked", Panel: render.Text("Locked panel"), Disabled: true},
	}}, nil)
	ctx := behaviorPage(t, startBehaviorServer(t, string(strip)))
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the strip marker never loaded headless-tabs")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#pf-tab-1`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(() => {
		const p = i => document.getElementById('pf-panel-' + i);
		return !p(1).hasAttribute('hidden') && p(1).tabIndex === 0 && !p(1).hasAttribute('aria-hidden') &&
			p(0).getAttribute('hidden') === 'until-found' && p(0).tabIndex === -1 && p(0).getAttribute('aria-hidden') === 'true' &&
			p(2).getAttribute('hidden') === '';
	})()`) {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`[0,1,2].map(i => { const p = document.getElementById('pf-panel-' + i);
			return i + ':' + p.getAttribute('hidden') + ':' + p.tabIndex + ':' + p.getAttribute('aria-hidden') }).join(' ')`, &state))
		t.Fatalf("the panels did not follow the selection: %s", state)
	}
}

func TestE2E_TabsFoundPanelSelectsItsTab(t *testing.T) {
	strip := Tabs(TabsProps{Name: "fs", Tabs: []Tab{
		{Label: "General", Panel: render.Text("General panel")},
		{Label: "Secrets", Panel: render.HTML(`<p id="needle">API key</p>`)},
		{Label: "Locked", Panel: render.HTML(`<p id="locked">Locked text</p>`), Disabled: true},
	}}, nil)
	ctx := behaviorPage(t, startBehaviorServer(t, string(strip)))
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the strip marker never loaded headless-tabs")
	}
	// A fragment link into the hidden panel: the browser fires
	// beforematch on it and drops hidden, as find-in-page does.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.hash = '#needle'`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('#fs-tab-1').getAttribute('aria-selected') === 'true' &&
		String(window.__gofastr.getSignal('fs')) === '1' &&
		document.querySelector('#needle').checkVisibility() &&
		document.querySelector('#fs-panel-0').getAttribute('hidden') === 'until-found'`) {
		t.Fatal("revealing the second panel did not select its tab and hide the first")
	}
	// The disabled tab's panel is plainly hidden, so the same link
	// cannot open it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.hash = '#locked'`, nil)); err != nil {
		t.Fatal(err)
	}
	var stillHidden bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!document.querySelector('#locked').checkVisibility() &&
		document.querySelector('#fs-tab-1').getAttribute('aria-selected') === 'true'`, &stillHidden)); err != nil {
		t.Fatal(err)
	}
	if !stillHidden {
		t.Fatal("a fragment link opened the disabled tab's panel")
	}
}

// A fragment URL on first load reaches a hidden panel before the module
// is listening: the browser fires beforematch to nobody and drops the
// panel's hidden. The module must adopt that panel, not hide it again,
// and a vacated panel must come back from its stash.
func TestE2E_TabsFragmentOnLoadSelectsItsTab(t *testing.T) {
	for _, tc := range []struct {
		name, frag string
		vacate     bool
	}{
		{"text in the panel", "#needle", false},
		{"the panel itself", "#ld-panel-1", false},
		{"a vacated panel", "#ld-panel-1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			strip := Tabs(TabsProps{Name: "ld", VacateHidden: tc.vacate, Tabs: []Tab{
				{Label: "General", Panel: render.Text("General panel")},
				{Label: "Secrets", Panel: render.HTML(`<p id="needle">API key</p>`)},
			}}, nil)
			b := startBehaviorServer(t, string(strip))
			ctx := behaviorBrowserCtx(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(b.srv.URL+"/"+tc.frag),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, tabsLoaded) {
				t.Fatal("the strip marker never loaded headless-tabs")
			}
			if !pollTrue(ctx, `document.querySelector('#ld-tab-1').getAttribute('aria-selected') === 'true' &&
				document.querySelector('[data-hui-tabs]').getAttribute('data-active') === '1' &&
				!document.querySelector('#ld-panel-1').hasAttribute('hidden') &&
				!!document.querySelector('#needle') && document.querySelector('#needle').checkVisibility() &&
				document.querySelector('#ld-panel-0').getAttribute('hidden') === 'until-found'`) {
				var state string
				_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({active: document.querySelector('[data-hui-tabs]').getAttribute('data-active'),
					p1: document.querySelector('#ld-panel-1').getAttribute('hidden'), needle: !!document.querySelector('#needle')})`, &state))
				t.Fatalf("loading onto %s did not select the second tab: %s", tc.frag, state)
			}
		})
	}
}

// With no script, a panel the browser reveals must not stay hidden
// from assistive technology: the server paints no aria-hidden, the
// module adds it to inactive panels once it runs.
func TestTabsServerPaintsNoAriaHidden(t *testing.T) {
	h := string(Tabs(TabsProps{Name: "na", Tabs: []Tab{
		{Label: "One", Panel: render.Text("one")},
		{Label: "Two", Panel: render.Text("two")},
	}}, nil))
	if strings.Contains(h, "aria-hidden") {
		t.Errorf("the server painted aria-hidden on a panel; a no-script reveal would leave it hidden from AT:\n%s", h)
	}
}
