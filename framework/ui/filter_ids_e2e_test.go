package ui_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestFilterToolbarScopesControlIDs(t *testing.T) {
	for _, mode := range []string{"inline", "dropdown", "request scope"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			if mode == "request scope" {
				ctx = render.WithIDScope(ctx)
			}
			var toolbars []render.HTML
			for index, id := range []string{"primary-filters", "secondary-filters"} {
				shortcut := ""
				if index == 1 {
					shortcut = "/"
				}
				if mode == "request scope" {
					id = ""
				}
				toolbars = append(toolbars, ui.FilterToolbar(ui.FilterToolbarConfig{
					Ctx: ctx, ID: id, Action: "/records", Dropdown: mode == "dropdown",
					Search: &ui.FilterSearch{Name: "q", Value: "client", Shortcut: shortcut},
					Facets: []ui.Facet{
						{Name: "status", Label: "Status", Kind: ui.FacetPills, Value: "open", Options: []ui.FacetOption{{Label: "Open", Value: "open"}, {Label: "Closed", Value: "closed"}}},
						{Name: "plan", Label: "Plan", Value: "pro", Options: []ui.FacetOption{{Label: "Pro", Value: "pro"}}},
					},
					Sort: []ui.SortOption{{Label: "Newest", Value: "newest"}}, SortValue: "newest",
				}))
			}
			server := themeTestPageWithHead(t, "", string(render.Join(toolbars...)))
			browser := moduleTestCtxURL(t, server.URL)
			var duplicates []string
			if err := chromedp.Run(browser, chromedp.Evaluate(`(() => { const seen = new Set(); return [...document.querySelectorAll('[id]')].map(node => node.id).filter(id => seen.has(id) || !seen.add(id)); })()`, &duplicates)); err != nil {
				t.Fatal(err)
			}
			if len(duplicates) > 0 {
				t.Fatalf("toolbar controls share IDs: %q", duplicates)
			}
			var labelFocus, submitted bool
			if err := chromedp.Run(browser, chromedp.Evaluate(`(() => { const form = document.querySelectorAll('form')[1]; const input = form.querySelector('input[type="search"]'); form.querySelector('label[for="' + input.id + '"]').click(); return document.activeElement === input; })()`, &labelFocus), chromedp.Evaluate(`(() => { const data = new FormData(document.querySelectorAll('form')[1]); return data.get('q') === 'client' && data.get('status') === 'open' && data.get('plan') === 'pro' && data.get('sort') === 'newest'; })()`, &submitted), chromedp.Evaluate(`document.activeElement.blur()`, nil), chromedp.KeyEvent("/")); err != nil {
				t.Fatal(err)
			}
			if !labelFocus || !submitted {
				t.Fatalf("second search label focuses own input: %v; unchanged native form names: %v", labelFocus, submitted)
			}
			if !pollJS(browser, `document.activeElement === document.querySelectorAll('form')[1].querySelector('input[type="search"]')`) {
				t.Fatal("second toolbar's shortcut targets another search input")
			}
		})
	}
}
