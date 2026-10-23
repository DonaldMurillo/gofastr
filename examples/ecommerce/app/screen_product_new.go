//gofastr:generated generator=app kind=scaffold version=dev sum=e79fd04db6d1f190

package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	productNewScreenTextAddProduct        = i18n.Define("shopfront.screens.product_new.add_product", "Add Product")
	productNewScreenTextCreateANewProduct = i18n.Define("shopfront.screens.product_new.create_a_new_product", "Create a new product listing")
	productNewScreenTextAddNewProduct     = i18n.Define("shopfront.screens.product_new.add_new_product", "Add New Product")
	productNewScreenTextCreate            = i18n.Define("shopfront.screens.product_new.create", "Create")
	productNewScreenTextNewProduct        = i18n.Define("shopfront.screens.product_new.new_product", "New Product")
)

type ProductNewScreen struct{ component.ContextOnly }

func (s *ProductNewScreen) ScreenTitle(ctx context.Context) string {
	return productNewScreenTextAddProduct.Text(ctx)
}
func (s *ProductNewScreen) ScreenDescription(ctx context.Context) string {
	return productNewScreenTextCreateANewProduct.Text(ctx)
}
func (s *ProductNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(productNewScreenTextAddNewProduct.Text(ctx))),
		render.Join(ui.PageHeader(ui.PageHeaderConfig{Title: productNewScreenTextNewProduct.Text(ctx)}), ui.Form(ui.FormConfig{Action: "/api/products", Method: "POST", SubmitLabel: productNewScreenTextCreate.Text(ctx), ExtraAttrs: html.MergeAttrs(html.Attrs{"data-entity-form": "products", "data-entity-mode": "create"}, interactive.Post("/api/products").OnSuccess(interactive.ResetForm()).Attrs())}, ui.FormField(ui.FormFieldConfig{Label: i18nui.FieldLabel(ctx, nil, "products", "name", "Name"), For: "field-name", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "name"})
		}}), ui.FormField(ui.FormFieldConfig{Label: i18nui.FieldLabel(ctx, nil, "products", "slug", "Slug"), For: "field-slug", Required: true, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "slug"})
		}}), ui.FormField(ui.FormFieldConfig{Label: i18nui.FieldLabel(ctx, nil, "products", "sku", "SKU"), For: "field-sku", Required: false, Input: func(c headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: "sku"})
		}}), ui.TextArea(ui.TextAreaConfig{Name: "description", Label: i18nui.FieldLabel(ctx, nil, "products", "description", "Description"), ID: "field-description", Required: false}), ui.NumberField(ui.NumberFieldConfig{Name: "price", Label: i18nui.FieldLabel(ctx, nil, "products", "price", "Price"), ID: "field-price", Required: true}), ui.NumberField(ui.NumberFieldConfig{Name: "stock", Label: i18nui.FieldLabel(ctx, nil, "products", "stock", "Stock"), ID: "field-stock", Required: true}), ui.Select(ui.SelectConfig{Name: "status", Label: i18nui.FieldLabel(ctx, nil, "products", "status", "Status"), ID: "field-status", Placeholder: i18nui.EntitySelect.Text(ctx), Options: []ui.SelectOption{{Value: "draft", Text: i18nui.FieldValueLabel(ctx, nil, "products", "status", "draft")}, {Value: "active", Text: i18nui.FieldValueLabel(ctx, nil, "products", "status", "active")}, {Value: "archived", Text: i18nui.FieldValueLabel(ctx, nil, "products", "status", "archived")}}, Required: false}), ui.Checkbox(ui.ToggleConfig{Name: "featured", Label: i18nui.FieldLabel(ctx, nil, "products", "featured", "Featured"), ID: "field-featured", Required: false}))),
	)
}

// mountProductNewScreen mounts the product_new screen with site.
func mountProductNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/new-product", &ProductNewScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars, screenRegistrar{order: 5, fn: mountProductNewScreen})
}
