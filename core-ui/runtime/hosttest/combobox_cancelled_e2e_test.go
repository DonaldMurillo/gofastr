package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestCancelledComboboxLinkStaysPut(t *testing.T) {
	for _, rows := range []string{"ssr", "island", "search"} {
		for _, signal := range []bool{false, true} {
			for _, absolute := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/signal=%t/absolute=%t", rows, signal, absolute), func(t *testing.T) {
					server := comboboxLinkSite(t, rows, "", "")
					ctx := comboboxLinkPage(t, server, rows, "")
					recordComboboxHistory(t, ctx)
					destination := "/cedar?view=branch#needles"
					if absolute {
						destination = server.URL + destination
					}
					const option = `#links-listbox a[href="/birch?view=tree#leaves"]`
					var label string
					if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`
						const option = document.querySelector('%s');
						option.setAttribute('href', %q);
						option.setAttribute('data-cui-push-state', %q);
						if (%t) option.setAttribute('data-cui-signal-inc', 'cancel-count');
						option.addEventListener('click', event => {
							window.cancelledClickObserved = event.isTrusted && event.button === 0;
							event.preventDefault();
						});
						option.getAttribute('data-value');
					`, option, destination, destination, signal), &label)); err != nil {
						t.Fatal(err)
					}
					var state struct {
						Path         string
						Pushes       int
						Replaces     int
						Navigations  int
						Programmatic int
						Observed     bool
						Picked       bool
					}
					if err := chromedp.Run(ctx,
						chromedp.Click(`#links-listbox a[href="`+destination+`"] span`),
						chromedp.Evaluate(fmt.Sprintf(`({
							Path: location.pathname + location.search + location.hash,
							Pushes: historyPushes, Replaces: historyReplaces, Navigations: navEvents,
							Programmatic: programmaticPicks, Observed: cancelledClickObserved,
							Picked: pickedInput.value === %q && pickedList.hidden
						})`, label), &state),
					); err != nil {
						t.Fatal(err)
					}
					if state.Path != "/" || state.Pushes != 0 || state.Replaces != 0 || state.Navigations != 0 || state.Programmatic != 0 || !state.Observed || !state.Picked {
						t.Errorf("cancelled click was not preserved: %+v", state)
					}
				})
			}
		}
	}
}
