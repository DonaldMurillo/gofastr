package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ProductNewScreen struct{}

func (s *ProductNewScreen) ScreenTitle() string        { return "Add Product" }
func (s *ProductNewScreen) ScreenDescription() string  { return "Create a new product listing" }
func (s *ProductNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductNewScreen) Render() render.HTML {
	return html.Div(html.DivConfig{},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Add New Product")),
		render.Join(ui.PageHeader(ui.PageHeaderConfig{Title: "New Product"}), ui.Form(ui.FormConfig{Action: "/api/products", Method: "POST", SubmitLabel: "Create", ExtraAttrs: html.MergeAttrs(html.Attrs{"data-entity-form": "products", "data-entity-mode": "create"}, interactive.Post("/api/products").OnSuccess(interactive.ResetForm()).Attrs())}, ui.FormField(ui.FormFieldConfig{Label: "Name", For: "field-name", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "name"})
		}}), ui.FormField(ui.FormFieldConfig{Label: "Slug", For: "field-slug", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "slug"})
		}}), ui.FormField(ui.FormFieldConfig{Label: "SKU", For: "field-sku", Required: false, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "sku"})
		}}), ui.TextArea(ui.TextAreaConfig{Name: "description", Label: "Description", ID: "field-description", Required: false}), ui.NumberField(ui.NumberFieldConfig{Name: "price", Label: "Price", ID: "field-price", Required: true}), ui.NumberField(ui.NumberFieldConfig{Name: "stock", Label: "Stock", ID: "field-stock", Required: true}), ui.Select(ui.SelectConfig{Name: "status", Label: "Status", ID: "field-status", Placeholder: "— Select —", Options: []ui.SelectOption{{Value: "draft", Text: "Draft"}, {Value: "active", Text: "Active"}, {Value: "archived", Text: "Archived"}}, Required: false}), ui.Checkbox(ui.ToggleConfig{Name: "featured", Label: "Featured", ID: "field-featured", Required: false}))),
	)
}

// mountProductNewScreen mounts the product_new screen with site.
func mountProductNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/new-product", &ProductNewScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 5, fn: mountProductNewScreen})
}
