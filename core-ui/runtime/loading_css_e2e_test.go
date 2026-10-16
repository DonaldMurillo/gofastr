package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestLoadingMinSpansSheetWait: a loading region whose After timer
// fires while the navigation waits for its component sheets still
// holds for its Min and leaves through its exit before the content
// goes in. The hold once ran before the sheet wait, saw nothing shown
// yet, and the commit then swept the skeleton out early. A cached
// replay and a prefetched entry wait for sheets too, so they honor the
// same hold: /x is the warm-up's cached page, /x3 is fetched, and /d
// is served from a prefetch entry the test plants.
func TestLoadingMinSpansSheetWait(t *testing.T) {
	const plant = `const take = G._takePrefetched;
		G._takePrefetched = (p) => p === '/d'
			? { html: '<template data-cui-fill="l:x">MAIN-PF</template><template data-cui-fill="l:x#aside">ASIDE-PF</template>', envelope: true, layer: 'l:x', title: 'PF', at: Date.now() }
			: take && take(p);`
	for _, arm := range []struct{ name, link, main, setup string }{
		{"fetched", "#goX3", "MAIN-X3", ""},
		{"cached", "#goX", "BOOT-X", ""},
		{"prefetched", "#goD", "MAIN-PF", plant},
	} {
		t.Run(arm.name, func(t *testing.T) {
			tpl := `<template data-cui-loading="l:x" data-cui-after="150" data-cui-min="1000"><span id="skeleton">SKELETON</span></template>`
			r := newGapRig(t, tpl, "", "")
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			warmModule(t, ctx, r)
			var shownFor float64
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const G = window.__gofastr; `+arm.setup+`
					G._cssReady = () => new Promise((r) => setTimeout(r, 700));
					window.__sk = 0; window.__main = 0;
					new MutationObserver(() => {
						if (!window.__sk && document.getElementById('skeleton')) window.__sk = performance.now();
						if (!window.__main && !document.getElementById('skeleton') && document.getElementById('main').textContent.includes('`+arm.main+`')) window.__main = performance.now();
					}).observe(document.body, { childList: true, subtree: true, characterData: true });
					return true; })()`, nil),
				chromedp.Click(arm.link, chromedp.ByQuery),
				chromedp.Poll(`window.__main > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(`window.__sk ? window.__main - window.__sk : -1`, &shownFor),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if shownFor < 0 {
				t.Fatal("the skeleton never showed: the test needs its After timer to fire during the sheet wait")
			}
			if shownFor < 950 {
				t.Fatalf("the skeleton gave way after %.0fms, want its 1000ms Min", shownFor)
			}
		})
	}
}
