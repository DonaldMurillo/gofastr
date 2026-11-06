package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestLanguageDemoHasNoDeadLinks(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	var choices, deadLinks int
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/components/languagemenu"), pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-menu-radio="language"]').length`, &choices),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('a[hreflang]')).filter(link=>/^\/(es|pt-BR)(\/|$)/.test(link.getAttribute('href'))).length`, &deadLinks),
	); err != nil {
		t.Fatal(err)
	}
	if choices != 3 {
		t.Fatalf("language preview has %d choices, want 3", choices)
	}
	if deadLinks != 0 {
		t.Fatalf("language preview has %d unserved locale links", deadLinks)
	}
}
