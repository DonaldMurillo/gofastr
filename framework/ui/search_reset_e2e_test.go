package ui_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestSearchClearTracksFormReset(t *testing.T) {
	for _, initial := range []string{"server search", ""} {
		t.Run(initial, func(t *testing.T) {
			input := ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "query", ExtraAttrs: map[string]string{"value": initial}})
			ctx := moduleTestCtx(t, `<form id="search-form">`+string(input)+`</form>`)
			if !pollJS(ctx, moduleLoaded("searchinput")) {
				t.Fatal("searchinput did not load")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const input = document.getElementById('query'); input.value = input.defaultValue ? '' : 'new search'; input.dispatchEvent(new Event('input', {bubbles:true})); document.getElementById('search-form').reset(); })()`, nil),
				chromedp.Poll(`document.querySelector('.fui-search__clear').hidden === (document.getElementById('query').value === '')`, nil, chromedp.WithPollingTimeout(time.Second)),
			); err != nil {
				t.Fatalf("reset left clear-button visibility stale: %v", err)
			}
		})
	}
}
