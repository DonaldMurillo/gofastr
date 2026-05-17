package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// DemoDrawerScreen is a side panel drawer with quick navigation links.
type DemoDrawerScreen struct{}

func (s *DemoDrawerScreen) ScreenTitle() string        { return "Quick Nav" }
func (s *DemoDrawerScreen) ScreenDescription() string  { return "Drawer overlay demo" }
func (s *DemoDrawerScreen) ScreenType() app.ScreenType { return app.ScreenDrawer }

func (s *DemoDrawerScreen) Render() render.HTML {
	return html.Div(html.DivConfig{Class: "drawer-content"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Quick Nav")),
		html.Paragraph(html.TextConfig{}, render.Text("A drawer slides in from the side. Great for navigation, filters, or settings.")),
		html.Nav(html.NavConfig{Label: "Drawer navigation"},
			html.UnorderedList(html.ListConfig{Class: "drawer-nav-list"},
				html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/", Text: "Home"})),
				html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/products", Text: "Products"})),
				html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/about", Text: "About"})),
				html.ListItem(html.ListItemConfig{}, html.Link(html.LinkConfig{Href: "/signals", Text: "Signal Demo"})),
			),
		),
	)
}

// DemoSheetScreen is a bottom sheet for quick product preview.
type DemoSheetScreen struct{}

func (s *DemoSheetScreen) ScreenTitle() string        { return "Product Preview" }
func (s *DemoSheetScreen) ScreenDescription() string  { return "Sheet overlay demo" }
func (s *DemoSheetScreen) ScreenType() app.ScreenType { return app.ScreenSheet }

func (s *DemoSheetScreen) Render() render.HTML {
	return html.Div(html.DivConfig{Class: "sheet-content"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Quick Preview")),
		html.Paragraph(html.TextConfig{}, render.Text("A sheet slides up from the bottom. Perfect for previews, action sheets, or quick forms.")),
		html.Div(html.DivConfig{Class: "sheet-product-preview"},
			html.Heading(html.HeadingConfig{Level: 3}, render.Text("Widget Pro")),
			html.Paragraph(html.TextConfig{}, render.Text(fmt.Sprintf("%s%.2f", "$", 29.99))),
			html.Paragraph(html.TextConfig{}, render.Text("High-quality widget with premium features. Add to cart from the products page.")),
		),
	)
}

// ConfirmDialogScreen is a modal dialog that asks the user to confirm an action.
type ConfirmDialogScreen struct {
	Message   string
	OnConfirm string
}

func (s *ConfirmDialogScreen) ScreenTitle() string        { return "Confirm" }
func (s *ConfirmDialogScreen) ScreenDescription() string  { return "Confirmation dialog" }
func (s *ConfirmDialogScreen) ScreenType() app.ScreenType { return app.ScreenDialog }

func (s *ConfirmDialogScreen) Render() render.HTML {
	return html.Div(html.DivConfig{Class: "confirm-dialog-content"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Confirm Action")),
		html.Paragraph(html.TextConfig{}, render.Text(s.Message)),
		html.Div(html.DivConfig{Class: "dialog-actions"},
			html.Button(html.ButtonConfig{
				Label: "Cancel",
				Class: "dialog-cancel-btn",
				Attrs: html.Attrs{
					"data-overlay-close": "",
					"data-fui-action":    "close",
				},
			}),
			html.Button(html.ButtonConfig{
				Label: "Confirm",
				Class: "cta-button confirm-btn",
				Attrs: html.Attrs{
					"data-action":        "confirm-action",
					"data-overlay-close": "",
					"data-fui-action":    "close",
				},
			}),
		),
	)
}
