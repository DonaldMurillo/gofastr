package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{ component.ContextOnly }

func (s *HomeScreen) ScreenTitle() string        { return "ShopFront: Home" }
func (s *HomeScreen) ScreenDescription() string  { return "E-commerce storefront homepage" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("ShopFront")),
		html.Paragraph(html.TextConfig{Class: ""}, render.Text("Welcome to our store. Browse our products and categories.")),
		appUI.List("products").Columns("name", "price", "status").PageSize(8).NoCreate().Heading("Featured Products", 2).Empty("No products available yet.").Base("/products").RenderCtx(ctx),
	)
}

type ProductsScreen struct{ component.ContextOnly }

func (s *ProductsScreen) ScreenTitle() string        { return "All Products" }
func (s *ProductsScreen) ScreenDescription() string  { return "Browse our full product catalog" }
func (s *ProductsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Products")),
		appUI.List("products").Columns("name", "price", "status", "stock").PageSize(20).NoCreate().Heading("Product Catalog", 2).Empty("No products found.").RenderCtx(ctx),
	)
}

type ProductDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *ProductDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *ProductDetailScreen) ScreenTitle() string           { return "Product Details" }
func (s *ProductDetailScreen) ScreenDescription() string     { return "View product details" }
func (s *ProductDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *ProductDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Product Details")),
		appUI.Record("products", s.id).Delete().Duplicate().RenderCtx(ctx),
	)
}

func mountHomeScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/", &HomeScreen{}, appLayout)
}

func mountProductsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/products", &ProductsScreen{}, appLayout)
}

func mountProductDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	record := app.NewScreen("/products/:id", &ProductDetailScreen{}).WithTitle("Product Details").WithDescription("View product details")
	record.Intercept = &app.Intercept{From: "/products", As: app.ScreenDrawer}
	site.RegisterScreen(record, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 0, fn: mountHomeScreen},
		screenRegistrar{order: 1, fn: mountProductsScreen},
		screenRegistrar{order: 6, fn: mountProductDetailScreen},
	)
}
