package headless

import (
	"context"
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

func TestComboboxEnterNeverSubmits(t *testing.T) {
	box := Combobox(ComboboxProps{ID: "search", Name: "q", Label: "Search",
		Island: &Island{Endpoint: "/results", Signal: "results"}, NoScriptAction: "/search",
	}, nil)
	server := startBehaviorServer(t, string(box), func(mux *http.ServeMux) {
		mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(ComboboxRows("search-listbox", []ComboboxOption{{Label: "Docs", Href: "/docs"}}, nil)))
		})
	})
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__submits = 0; document.querySelector('form').addEventListener('submit', event => { event.preventDefault(); window.__submits++; });`, nil),
		chromedp.Click(`#search`, chromedp.ByID),
		chromedp.SendKeys(`#search`, "docs", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!!document.querySelector('#search-listbox [role="option"]')`) {
		t.Fatal("island results did not arrive")
	}
	for _, state := range []string{"open", "closed", "empty"} {
		t.Run(state, func(t *testing.T) {
			var submits int
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const input = document.getElementById('search'); input.removeAttribute('aria-activedescendant'); if ('`+state+`' === 'closed') input.setAttribute('aria-expanded', 'false'); if ('`+state+`' === 'empty') { document.getElementById('search-listbox').replaceChildren(); input.setAttribute('aria-expanded', 'true'); } })()`, nil),
				chromedp.ActionFunc(func(ctx context.Context) error {
					return input.DispatchKeyEvent(input.KeyDown).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).WithText("\r").Do(ctx)
				}),
				chromedp.Evaluate(`window.__submits`, &submits),
			); err != nil {
				t.Fatal(err)
			}
			if submits != 0 {
				t.Fatalf("Enter submitted the no-script form %d times", submits)
			}
		})
	}
}
