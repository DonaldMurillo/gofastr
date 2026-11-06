package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestSignalComboboxLinkNavigates(t *testing.T) {
	for _, absolute := range []bool{true, false} {
		t.Run(fmt.Sprintf("absolute=%t", absolute), func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", "", "")
			ctx := comboboxLinkPage(t, server, "ssr", "")
			recordComboboxHistory(t, ctx)
			destination := "/birch?view=tree#leaves"
			if absolute {
				destination = server.URL + destination
			}
			var landed string
			var selected bool
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(fmt.Sprintf(`
					const option = document.querySelector('#links-listbox a');
					option.setAttribute('href', %q); option.setAttribute('data-cui-push-state', %q);
					option.setAttribute('data-cui-signal-set', 'selected:yes');
				`, destination, destination), nil),
				chromedp.Click(`#links-listbox a span`),
				chromedp.Evaluate(`location.pathname + location.search + location.hash`, &landed),
				chromedp.Evaluate(`__gofastr.getSignal('selected') === 'yes' && pickedInput.value === 'Birch' && pickedList.hidden && historyPushes === 1`, &selected),
			); err != nil {
				t.Fatal(err)
			}
			if landed != "/birch?view=tree#leaves" || !selected {
				t.Fatalf("signal-bearing pick landed on %q, committed signal/value/list/history=%t", landed, selected)
			}
			if err := chromedp.Run(ctx, chromedp.Poll(`navEvents === 1 && !!document.getElementById('leaves')`, nil)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
