package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ProductNewScreen struct{}

func (s *ProductNewScreen) ScreenTitle() string        { return "Add Product" }
func (s *ProductNewScreen) ScreenDescription() string  { return "Create a new product listing" }
func (s *ProductNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductNewScreen) Render() render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Add New Product")),
		render.Join(ui.PageHeader(ui.PageHeaderConfig{Title: "New Product"}), ui.Form(ui.FormConfig{Action: "/api/products", Method: "POST", SubmitLabel: "Create", ExtraAttrs: html.Attrs{"data-entity-form": "products", "data-entity-mode": "create", "data-fui-rpc": "/api/products", "data-fui-rpc-method": "POST", "data-fui-rpc-reset": "true"}}, ui.FormField(ui.FormFieldConfig{Label: "Name", For: "field-name", Required: true, Input: render.Raw("<input type=\"text\" name=\"name\" id=\"field-name\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Slug", For: "field-slug", Required: true, Input: render.Raw("<input type=\"text\" name=\"slug\" id=\"field-slug\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "SKU", For: "field-sku", Required: false, Input: render.Raw("<input type=\"text\" name=\"sku\" id=\"field-sku\">")}), ui.FormField(ui.FormFieldConfig{Label: "Description", For: "field-description", Required: false, Input: render.Raw("<textarea name=\"description\" id=\"field-description\"></textarea>")}), ui.FormField(ui.FormFieldConfig{Label: "Price", For: "field-price", Required: true, Input: render.Raw("<input type=\"number\" name=\"price\" id=\"field-price\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Stock", For: "field-stock", Required: true, Input: render.Raw("<input type=\"number\" name=\"stock\" id=\"field-stock\" required>")}), ui.FormField(ui.FormFieldConfig{Label: "Status", For: "field-status", Required: false, Input: render.Raw("<select name=\"status\" id=\"field-status\"><option value=\"\">— Select —</option><option value=\"draft\">Draft</option><option value=\"active\">Active</option><option value=\"archived\">Archived</option></select>")}), ui.FormField(ui.FormFieldConfig{Label: "Featured", For: "field-featured", Required: false, Input: render.Raw("<input type=\"checkbox\" name=\"featured\" id=\"field-featured\">")}))),
	)
}

// mountProductNewScreen mounts the product_new screen with site.
func mountProductNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/new-product", &ProductNewScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 5, fn: mountProductNewScreen})
}
