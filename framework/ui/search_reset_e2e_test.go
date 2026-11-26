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

func TestSearchClearWithNamedFormControl(t *testing.T) {
	for _, tc := range []struct{ name, control, clear, legacy string }{
		{name: "click", control: "requestSubmit", clear: `document.querySelector('.fui-search__clear').click()`},
		{name: "Escape with legacy submit", control: "submit",
			clear: `input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}))`,
			legacy: `HTMLFormElement.prototype.requestSubmit = undefined;
				HTMLFormElement.prototype.submit = function () { record(this); };`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := ui.SearchInput(ui.SearchInputConfig{
				Name: tc.control, ID: "query", Action: "/search", ExtraAttrs: map[string]string{"value": "books"},
			})
			ctx := moduleTestCtx(t, string(input))
			if !pollJS(ctx, moduleLoaded("searchinput")) {
				t.Fatal("searchinput did not load")
			}
			var got string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const input = document.getElementById('query');
				history.replaceState(null, '', '?' + input.name + '=books');
				let submitted = 'missing';
				const record = form => { submitted = JSON.stringify(Array.from(new FormData(form))); };
				input.form.addEventListener('submit', event => { event.preventDefault(); record(event.target); });
				`+tc.legacy+`
				`+tc.clear+`;
				return submitted;
			})()`, &got)); err != nil {
				t.Fatal(err)
			}
			want := `[["` + tc.control + `",""]]`
			if got != want {
				t.Fatalf("clear submitted %s, want %s", got, want)
			}
		})
	}
}
