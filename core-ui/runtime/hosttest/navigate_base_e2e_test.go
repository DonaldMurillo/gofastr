package hosttest

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestNavigateRespectsDocumentBase(t *testing.T) {
	for _, base := range []string{"/birch", "cedar", "/birch?ignored=1#old"} {
		t.Run(base, func(t *testing.T) {
			server := comboboxLinkSite(t, "ssr", "", "")
			ctx := comboboxLinkPage(t, server, "ssr", "")
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/?view=tree"),
				chromedp.Poll(`!!window.__gofastr?.navigate`, nil),
				chromedp.Evaluate(fmt.Sprintf(`const base = document.createElement('base'); base.href = %q; document.head.prepend(base)`, base), nil),
			); err != nil {
				t.Fatal(err)
			}
			recordComboboxHistory(t, ctx)
			var expected string
			var destination string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`new URL('?view=tree', document.baseURI).href`, &expected),
				chromedp.Evaluate(`__gofastr.navigate('?view=tree'); location.href`, &destination),
			); err != nil {
				t.Fatal(err)
			}
			if destination != expected {
				t.Fatalf("navigate with base=%q left %q, want %q", base, destination, expected)
			}
			var pushes int
			if err := chromedp.Run(ctx,
				chromedp.Poll(`navEvents === 1 && !!document.querySelector('#leaves,#needles')`, nil),
				chromedp.Evaluate(`__gofastr.navigate('?view=tree'); historyPushes`, &pushes),
			); err != nil {
				t.Fatal(err)
			}
			if pushes != 1 {
				t.Errorf("base-relative navigation wrote %d entries, want 1", pushes)
			}
		})
	}
}
