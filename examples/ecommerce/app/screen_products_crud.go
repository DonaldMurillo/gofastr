//gofastr:generated generator=app kind=scaffold version=dev sum=cf7bbf917de3fa00

package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	productsCrudTextShopfrontHome               = i18n.Define("shopfront.screens.products_crud.shopfront_home", "ShopFront: Home")
	productsCrudTextECommerceStorefrontHomepage = i18n.Define("shopfront.screens.products_crud.e_commerce_storefront_homepage", "E-commerce storefront homepage")
	productsCrudTextShopfront                   = i18n.Define("shopfront.screens.products_crud.shopfront", "ShopFront")
	productsCrudTextWelcomeToOurStore           = i18n.Define("shopfront.screens.products_crud.welcome_to_our_store", "Welcome to our store. Browse our products and categories.")
	productsCrudTextFeaturedProducts            = i18n.Define("shopfront.screens.products_crud.featured_products", "Featured Products")
	productsCrudTextNoProductsAvailableYet      = i18n.Define("shopfront.screens.products_crud.no_products_available_yet", "No products available yet.")
	productsCrudTextAllProducts                 = i18n.Define("shopfront.screens.products_crud.all_products", "All Products")
	productsCrudTextBrowseOurFullProduct        = i18n.Define("shopfront.screens.products_crud.browse_our_full_product", "Browse our full product catalog")
	productsCrudTextProducts                    = i18n.Define("shopfront.screens.products_crud.products", "Products")
	productsCrudTextProductCatalog              = i18n.Define("shopfront.screens.products_crud.product_catalog", "Product Catalog")
	productsCrudTextNoProductsFound             = i18n.Define("shopfront.screens.products_crud.no_products_found", "No products found.")
	productsCrudTextProductDetails              = i18n.Define("shopfront.screens.products_crud.product_details", "Product Details")
	productsCrudTextViewProductDetails          = i18n.Define("shopfront.screens.products_crud.view_product_details", "View product details")
)

type HomeScreen struct{ component.ContextOnly }

func (s *HomeScreen) ScreenTitle(ctx context.Context) string {
	return productsCrudTextShopfrontHome.Text(ctx)
}
func (s *HomeScreen) ScreenDescription(ctx context.Context) string {
	return productsCrudTextECommerceStorefrontHomepage.Text(ctx)
}
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(productsCrudTextShopfront.Text(ctx))),
		html.Paragraph(html.TextConfig{Class: ""}, render.Text(productsCrudTextWelcomeToOurStore.Text(ctx))),
		appUI.List("products").Columns("name", "price", "status").PageSize(8).NoCreate().Heading(productsCrudTextFeaturedProducts.Text(ctx), 2).Empty(productsCrudTextNoProductsAvailableYet.Text(ctx)).Base("/products").RenderCtx(ctx),
	)
}

type ProductsScreen struct{ component.ContextOnly }

func (s *ProductsScreen) ScreenTitle(ctx context.Context) string {
	return productsCrudTextAllProducts.Text(ctx)
}
func (s *ProductsScreen) ScreenDescription(ctx context.Context) string {
	return productsCrudTextBrowseOurFullProduct.Text(ctx)
}
func (s *ProductsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductsScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(productsCrudTextProducts.Text(ctx))),
		appUI.List("products").Columns("name", "price", "status", "stock").PageSize(20).NoCreate().Heading(productsCrudTextProductCatalog.Text(ctx), 2).Empty(productsCrudTextNoProductsFound.Text(ctx)).RenderCtx(ctx),
	)
}

type ProductDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *ProductDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *ProductDetailScreen) ScreenTitle(ctx context.Context) string {
	return productsCrudTextProductDetails.Text(ctx)
}
func (s *ProductDetailScreen) ScreenDescription(ctx context.Context) string {
	return productsCrudTextViewProductDetails.Text(ctx)
}
func (s *ProductDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(productsCrudTextProductDetails.Text(ctx))),
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
