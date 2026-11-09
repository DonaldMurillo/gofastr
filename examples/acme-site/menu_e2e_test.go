package main

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The phone menu keeps Tab inside itself while it is open, and its
// close button is one of the stops: Tab used to cycle the panel's links
// and never reach the toggle, so a keyboard reader could not close it
// without knowing Escape. The toggle is drawn on screen with a name,
// and Escape closes the menu with focus back on it.
func TestPhoneMenuTabReachesClose(t *testing.T) {
	base := acmeServe(t)
	ctx := acmeBrowser(t, 390, 800)
	const menu = `document.querySelector('[data-cui-scope="siteheader"] details')`
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/")); err != nil {
		t.Fatal(err)
	}
	// The disclosure module mirrors the open state and holds the trap;
	// a tap before it loads opens the bare <details>.
	waitUntil(t, ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-disclosure'])`)
	if err := chromedp.Run(ctx, chromedp.Click(`[data-cui-scope="siteheader"] summary`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, menu+`.open && document.querySelector('[data-cui-scope="siteheader"] summary').getAttribute('aria-expanded') === 'true'`)

	reachedClose, reachedLink := false, false
	for i := 0; i < 12; i++ {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
			t.Fatal(err)
		}
		where := evalString(t, ctx, `(() => {
			const a = document.activeElement, m = `+menu+`;
			if (!m.contains(a)) return 'outside: ' + a.tagName + ' ' + a.textContent.trim().slice(0, 30);
			return a.tagName === 'SUMMARY' ? 'close' : 'inside';
		})()`)
		switch where {
		case "close":
			reachedClose = true
		case "inside":
			reachedLink = true
		default:
			t.Fatalf("Tab %d left the open menu: %s", i+1, where)
		}
	}
	if !reachedClose || !reachedLink {
		t.Fatalf("Tab should walk the menu's links and its close button: link %v, close %v", reachedLink, reachedClose)
	}

	// The close button is drawn and named: its centre hits it, and a
	// screen reader hears a name and the expanded state.
	if got := evalString(t, ctx, `(() => {
		const s = document.querySelector('[data-cui-scope="siteheader"] summary');
		const r = s.getBoundingClientRect();
		if (r.width < 24 || r.height < 24) return 'close button is ' + r.width + 'x' + r.height;
		const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
		if (!s.contains(hit)) return 'close button is covered by ' + (hit ? hit.tagName : 'nothing');
		if (!s.getAttribute('aria-label')) return 'close button has no name';
		if (s.getAttribute('aria-expanded') !== 'true') return 'aria-expanded = ' + s.getAttribute('aria-expanded');
		return '';
	})()`); got != "" {
		t.Fatal(got)
	}

	// Escape closes it from a link inside, focus back on the toggle.
	if err := chromedp.Run(ctx,
		chromedp.Focus(`[data-cui-scope="siteheader"] details nav a`, chromedp.ByQuery),
		chromedp.KeyEvent(kb.Escape),
	); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, `!`+menu+`.open && document.activeElement === document.querySelector('[data-cui-scope="siteheader"] summary')`)
}
