package main

// The intercepting-route example (#130 slice 5): one detail screen that
// presents two ways depending on how you got to it.
//
//   - /examples/catalog        the list
//   - /examples/catalog/:id    the detail, a real, shareable, indexable
//                              page that renders standalone on a hard
//                              load, refresh, or external link
//
// Clicking a row on the list soft-navigates to the detail URL and the
// runtime presents it as a drawer over the list, which stays mounted
// behind it. Back, ESC, and the backdrop all close it and cost no
// refetch. Copy the URL out of the address bar mid-overlay and open it
// in a new tab: the same route renders as a full page.
//
// That split is the whole point, and it is why this is NOT a widget deep
// link. A deep link would keep the URL on the list
// (/examples/catalog?modal=…) and the detail would have no page of its
// own. Here the page IS the canonical render and the overlay is a
// presentation of it, the server decides which one you get, from the
// origin the client reports (see core-ui/app/intercept.go).

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type catalogItem struct {
	ID, Name, Category, Price, Blurb string
}

var catalogItems = []catalogItem{
	{ID: "kettle", Name: "Pour-over kettle", Category: "Brewing", Price: "$68", Blurb: "Gooseneck spout with a counterweighted handle. Holds a 60 ml/s pour without wrist strain."},
	{ID: "grinder", Name: "Hand grinder", Category: "Brewing", Price: "$95", Blurb: "Conical burrs, 36 clicks per rotation. Repeatable from espresso to French press."},
	{ID: "scale", Name: "Brew scale", Category: "Measurement", Price: "$42", Blurb: "0.1 g resolution with a built-in timer. Auto-tares when a vessel lands on it."},
	{ID: "server", Name: "Glass server", Category: "Serving", Price: "$34", Blurb: "600 ml borosilicate carafe. Reads clearly against a light background for judging extraction."},
}

func catalogByID(id string) (catalogItem, bool) {
	for _, it := range catalogItems {
		if it.ID == id {
			return it, true
		}
	}
	return catalogItem{}, false
}

// ── list ────────────────────────────────────────────────────────────

type CatalogScreen struct{ component.ContextOnly }

func (s *CatalogScreen) ScreenTitle() string { return "Catalog example" }
func (s *CatalogScreen) ScreenDescription() string {
	return "A list whose detail route opens as a drawer when you click through, and as a full page when you load its URL directly."
}
func (s *CatalogScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CatalogScreen) RenderCtx(_ context.Context) render.HTML {
	rows := make([]render.HTML, 0, len(catalogItems))
	for _, it := range catalogItems {
		rows = append(rows, html.LinkHTML(html.LinkHTMLConfig{
			Href:  "/examples/catalog/" + it.ID,
			Class: "cat-row",
			Content: render.Join(
				render.Tag("span", map[string]string{"class": "cat-row__name"}, render.Text(it.Name)),
				render.Tag("span", map[string]string{"class": "cat-row__meta"}, render.Text(it.Category+" \u00b7 "+it.Price)),
			),
		}))
	}

	return html.Div(html.DivConfig{Class: "cat-page"},
		ui.PageHeader(ui.PageHeaderConfig{
			Title: "Catalog",
			Subtitle: "Click a product: its own route opens as a drawer over this list. " +
				"Press Escape or Back to close. The list never reloaded. Open the same URL " +
				"in a new tab and it renders as a full page.",
		}),
		html.Div(html.DivConfig{Class: "cat-list"}, rows...),
	)
}

// ── detail ──────────────────────────────────────────────────────────

// CatalogItemScreen renders one product. Registered as an ordinary page
// with app.InterceptFrom, so this same method serves both the standalone
// page and the drawer, there is no second render path to keep in sync.
type CatalogItemScreen struct {
	component.ContextOnly
	id string
}

func (s *CatalogItemScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *CatalogItemScreen) ScreenType() app.ScreenType    { return app.ScreenPage }
func (s *CatalogItemScreen) ScreenTitle() string {
	if it, ok := catalogByID(s.id); ok {
		return it.Name
	}
	return "Product"
}
func (s *CatalogItemScreen) ScreenDescription() string {
	if it, ok := catalogByID(s.id); ok {
		return it.Blurb
	}
	return "Catalog product detail."
}

func (s *CatalogItemScreen) RenderCtx(_ context.Context) render.HTML {
	it, ok := catalogByID(s.id)
	if !ok {
		return html.Div(html.DivConfig{Class: "cat-detail"},
			ui.EmptyState(ui.EmptyStateConfig{
				Title:        "No such product",
				Description:  "That catalog id does not exist.",
				HeadingLevel: 2,
			}),
			html.Link(html.LinkConfig{Href: "/examples/catalog", Text: "Back to the catalog"}),
		)
	}

	return html.Div(html.DivConfig{Class: "cat-detail"},
		html.Heading(html.HeadingConfig{Level: 1, Class: "cat-detail__name"}, render.Text(it.Name)),
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "Category", Value: render.Text(it.Category)},
			{Label: "Price", Value: render.Text(it.Price)},
		}}),
		html.Paragraph(html.TextConfig{Class: "cat-detail__blurb"}, render.Text(it.Blurb)),
		// data-fui-intercept-close is inert on the standalone page and
		// closes the drawer when this render is the overlay, so one
		// markup tree serves both presentations.
		ui.Button(ui.ButtonConfig{
			Label:      "Close",
			Variant:    ui.ButtonGhost,
			ExtraAttrs: html.Attrs{"data-fui-intercept-close": ""},
		}),
	)
}
