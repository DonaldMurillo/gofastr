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
	return elements.Div(elements.Attrs{"class": "drawer-content"},
		elements.Heading(2, nil, render.Text("Quick Nav")),
		elements.Paragraph(nil, render.Text("A drawer slides in from the side. Great for navigation, filters, or settings.")),
		elements.Nav(nil,
			elements.UnorderedList(elements.Attrs{"class": "drawer-nav-list"},
				elements.ListItem(nil, elements.Link("/", "Home", nil)),
				elements.ListItem(nil, elements.Link("/products", "Products", nil)),
				elements.ListItem(nil, elements.Link("/about", "About", nil)),
				elements.ListItem(nil, elements.Link("/signals", "Signal Demo", nil)),
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
	return elements.Div(elements.Attrs{"class": "sheet-content"},
		elements.Heading(2, nil, render.Text("Quick Preview")),
		elements.Paragraph(nil, render.Text("A sheet slides up from the bottom. Perfect for previews, action sheets, or quick forms.")),
		elements.Div(elements.Attrs{"class": "sheet-product-preview"},
			elements.Heading(3, nil, render.Text("Widget Pro")),
			elements.Paragraph(nil, render.Text(fmt.Sprintf("%s%.2f", "$", 29.99))),
			elements.Paragraph(nil, render.Text("High-quality widget with premium features. Add to cart from the products page.")),
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
	return elements.Div(elements.Attrs{"class": "confirm-dialog-content"},
		elements.Heading(2, nil, render.Text("Confirm Action")),
		elements.Paragraph(nil, render.Text(s.Message)),
		elements.Div(elements.Attrs{"class": "dialog-actions"},
			elements.Button("Cancel", elements.Attrs{
				"class":              "dialog-cancel-btn",
				"data-overlay-close": "",
			}),
			elements.Button("Confirm", elements.Attrs{
				"class":              "cta-button confirm-btn",
				"data-action":        "confirm-action",
				"data-overlay-close": "",
			}),
		),
	)
}
