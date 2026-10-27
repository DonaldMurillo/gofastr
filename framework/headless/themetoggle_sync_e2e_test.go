package headless

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// Choosing a scheme in one theme toggle marks it in every toggle on
// the page: a header pill and a settings-page pill never disagree.
func TestE2E_ThemeToggleSyncsSiblings(t *testing.T) {
	group := func(id string) string {
		return `<div id="` + id + `" data-hui-theme-toggle="" role="radiogroup" aria-label="Colour scheme">` +
			`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="light">Light</button>` +
			`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="auto">Auto</button>` +
			`<button type="button" role="radio" aria-checked="false" data-hui-theme-option="dark">Dark</button>` +
			`</div>`
	}
	b := startBehaviorServer(t, group("one")+group("two"))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(NavigationBehaviorName)) {
		t.Fatal("the theme toggles never loaded headless-navigation")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#one [data-hui-theme-option="dark"]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("clicking Dark: %v", err)
	}
	const both = `(() => [...document.querySelectorAll('[data-hui-theme-option="dark"]')]
		.every(o => o.getAttribute('aria-checked') === 'true'))()`
	if !pollTrue(ctx, both) {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('[data-hui-theme-option]')].map(o => o.closest('[id]').id+':'+o.dataset.huiThemeOption+'='+o.getAttribute('aria-checked')).join(' ')`, &state))
		t.Fatalf("the second toggle kept its old choice: %s", strings.TrimSpace(state))
	}
}
