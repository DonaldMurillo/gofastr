package main

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// DemoDrawerScreen is a side panel drawer with quick navigation links.
type DemoDrawerScreen struct{}

func (s *DemoDrawerScreen) ScreenTitle() string        { return "Quick Nav" }
func (s *DemoDrawerScreen) ScreenDescription() string  { return "Drawer overlay demo" }
func (s *DemoDrawerScreen) ScreenType() app.ScreenType { return app.ScreenDrawer }

func (s *DemoDrawerScreen) Render() render.HTML {
	return elements.Div(elements.DivConfig{Class: "drawer-content"},
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Quick Nav")),
		elements.Paragraph(elements.TextConfig{}, render.Text("A drawer slides in from the side. Great for navigation, filters, or settings.")),
		elements.Nav(elements.NavConfig{Label: "Drawer navigation"},
			elements.UnorderedList(elements.ListConfig{Class: "drawer-nav-list"},
				elements.ListItem(elements.ListItemConfig{}, elements.Link(elements.LinkConfig{Href: "/", Text: "Home"})),
				elements.ListItem(elements.ListItemConfig{}, elements.Link(elements.LinkConfig{Href: "/products", Text: "Products"})),
				elements.ListItem(elements.ListItemConfig{}, elements.Link(elements.LinkConfig{Href: "/about", Text: "About"})),
				elements.ListItem(elements.ListItemConfig{}, elements.Link(elements.LinkConfig{Href: "/signals", Text: "Signal Demo"})),
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
	return elements.Div(elements.DivConfig{Class: "sheet-content"},
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Quick Preview")),
		elements.Paragraph(elements.TextConfig{}, render.Text("A sheet slides up from the bottom. Perfect for previews, action sheets, or quick forms.")),
		elements.Div(elements.DivConfig{Class: "sheet-product-preview"},
			elements.Heading(elements.HeadingConfig{Level: 3}, render.Text("Widget Pro")),
			elements.Paragraph(elements.TextConfig{}, render.Text(fmt.Sprintf("%s%.2f", "$", 29.99))),
			elements.Paragraph(elements.TextConfig{}, render.Text("High-quality widget with premium features. Add to cart from the products page.")),
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
	return elements.Div(elements.DivConfig{Class: "confirm-dialog-content"},
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Confirm Action")),
		elements.Paragraph(elements.TextConfig{}, render.Text(s.Message)),
		elements.Div(elements.DivConfig{Class: "dialog-actions"},
			elements.Button(elements.ButtonConfig{
				Label: "Cancel",
				Class: "dialog-cancel-btn",
				Attrs: elements.Attrs{"data-overlay-close": ""},
			}),
			elements.Button(elements.ButtonConfig{
				Label: "Confirm",
				Class: "cta-button confirm-btn",
				Attrs: elements.Attrs{
					"data-action":        "confirm-action",
					"data-overlay-close": "",
				},
			}),
		),
	)
}
