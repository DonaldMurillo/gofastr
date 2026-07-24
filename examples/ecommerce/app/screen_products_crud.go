package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type HomeScreen struct{ component.ContextOnly }

func (s *HomeScreen) ScreenTitle() string        { return "ShopFront — Home" }
func (s *HomeScreen) ScreenDescription() string  { return "E-commerce storefront homepage" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("ShopFront")),
		render.Tag("p", nil, render.Text("Welcome to our store. Browse our products and categories.")),
		appResources["products"].WithColumns("name", "price", "status").WithLimit(8).WithHeading("Featured Products").WithEmpty("No products available yet.").List(ctx),
	)
}

type ProductsScreen struct{ component.ContextOnly }

func (s *ProductsScreen) ScreenTitle() string        { return "All Products" }
func (s *ProductsScreen) ScreenDescription() string  { return "Browse our full product catalog" }
func (s *ProductsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Products")),
		appResources["products"].WithColumns("name", "price", "status", "stock").WithLimit(20).WithHeading("Product Catalog").WithEmpty("No products found.").List(ctx),
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
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Product Details")),
		appResources["products"].Detail(ctx, s.id),
	)
}

type ProductsEditScreen struct {
	component.ContextOnly
	id string
}

func (s *ProductsEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *ProductsEditScreen) ScreenTitle() string           { return "Edit Product" }
func (s *ProductsEditScreen) ScreenSEO() uihost.SEO         { return uihost.SEO{} } // deliberate SEO opt-out — set description in the blueprint or replace with real copy
func (s *ProductsEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *ProductsEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["products"].Form(ctx, s.id),
	)
}

func mountHomeScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["products"] = ResourceConfig{
		Title: "Products", Singular: "Product", BasePath: "/products", APIPath: "/api/products",
		Crud:    fwApp.MustCrudHandler("products"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "slug", Label: "Slug", Type: "string"},
			{Key: "sku", Label: "SKU", Type: "string"},
			{Key: "description", Label: "Description", Type: "text"},
			{Key: "price", Label: "Price", Type: "decimal"},
			{Key: "compare_at_price", Label: "Compare At Price", Type: "decimal"},
			{Key: "stock", Label: "Stock", Type: "int"},
			{Key: "category_id", Label: "Category", Type: "relation"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"draft", "active", "archived"}},
			{Key: "featured", Label: "Featured", Type: "bool"},
			{Key: "weight", Label: "Weight", Type: "float"},
			{Key: "image", Label: "Image", Type: "image"},
			{Key: "tags", Label: "Tags", Type: "json"},
		},
		Relations: map[string]RelSource{
			"category_id": {Crud: fwApp.MustCrudHandler("categories"), Display: "name"},
		},
		Related: []RelatedList{
			{
				Title: "Order Items", ForeignKey: "product_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("order_items"),
				Fields: []ResField{
					{Key: "user_id", Label: "User", Type: "string"},
					{Key: "order_id", Label: "Order", Type: "relation"},
					{Key: "product_name", Label: "Product Name", Type: "string"},
					{Key: "quantity", Label: "Quantity", Type: "int"},
				},
				Relations: map[string]RelSource{
					"order_id":   {Crud: fwApp.MustCrudHandler("orders"), Display: "user_id"},
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
			{
				Title: "Reviews", ForeignKey: "product_id", BasePath: "/reviews",
				Crud: fwApp.MustCrudHandler("reviews"),
				Fields: []ResField{
					{Key: "author_name", Label: "Author Name", Type: "string"},
					{Key: "rating", Label: "Rating", Type: "int"},
					{Key: "title", Label: "Title", Type: "string"},
					{Key: "body", Label: "Body", Type: "text"},
				},
				Relations: map[string]RelSource{
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
		},
	}
	site.Register("/", &HomeScreen{}, appLayout)
}

func mountProductsScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/products", &ProductsScreen{}, appLayout)
}

func mountProductDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/product-detail", &ProductDetailScreen{}, appLayout)
}

func mountProductsEditScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/product-detail/edit", &ProductsEditScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 0, fn: mountHomeScreen},
		screenRegistrar{order: 1, fn: mountProductsScreen},
		screenRegistrar{order: 6, fn: mountProductDetailScreen},
		screenRegistrar{order: 8, fn: mountProductsEditScreen},
	)
}
