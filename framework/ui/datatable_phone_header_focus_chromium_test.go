package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// On a phone the cards and rows modes clip the header row away, but its
// sort links stay in the tab order: Meridian's /app at 375px took five
// Tabs through controls nobody could see, with no focus ring anywhere
// on screen. A header that holds focus is drawn, so a keyboard user
// sees where focus is and can still sort.
func TestDataTablePhoneHeaderShownWhileFocused(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := `<meta name="viewport" content="width=device-width, initial-scale=1">` +
		"<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	for _, mode := range []ui.ResponsiveMode{ui.ResponsiveCards, ui.ResponsiveRows} {
		t.Run(string(mode), func(t *testing.T) {
			table := ui.DataTable(ui.DataTableConfig{
				Columns: []ui.Column{
					{Key: "name", Header: "Name", Sortable: true, Phone: ui.PhoneTitle},
					{Key: "mrr", Header: "MRR", Sortable: true, Align: "end", Phone: ui.PhoneDetail},
				},
				Responsive: mode,
				Rows: []ui.Row{{Cells: map[string]render.HTML{
					"name": render.Text("Ada"), "mrr": render.Text("$99.00"),
				}}},
			})
			body := `<button id="before">before</button>` + string(table)
			srv := themeTestPageWithHead(t, head, body)
			ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(375, 812))
			// Hit-testing the focused link's centre finds it only when it
			// is painted: a clipped element is not hit.
			probe := `(() => {
				const a = document.activeElement;
				if (!a || !a.classList.contains("fui-data-table__sort")) return "focus is on " + (a && a.outerHTML);
				const r = a.getBoundingClientRect();
				const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
				return hit && a.contains(hit) ? "" : "the focused sort link is not drawn: " + JSON.stringify(r);
			})()`
			var got, hidden string
			if err := chromedp.Run(ctx,
				chromedp.Focus("#before", chromedp.ByQuery),
				// The scroll region is the first stop, the sort link the next.
				chromedp.KeyEvent(kb.Tab),
				chromedp.KeyEvent(kb.Tab),
				chromedp.Evaluate(probe, &got),
				// Away from it, the header is clipped again.
				chromedp.Focus("#before", chromedp.ByQuery),
				chromedp.Evaluate(`(() => { const a = document.querySelector(".fui-data-table__sort"), r = a.getBoundingClientRect();
					const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
					return hit && a.contains(hit) ? "the unfocused header is drawn on a phone" : ""; })()`, &hidden),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if got != "" {
				t.Error(got)
			}
			if hidden != "" {
				t.Error(hidden)
			}
		})
	}
}
