package blueprint

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{ component.ContextOnly }

func (s *HomeScreen) ScreenTitle() string        { return "ShopFront — Home" }
func (s *HomeScreen) ScreenDescription() string  { return "E-commerce storefront homepage" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("ShopFront")),
		render.Tag("p", nil, render.Text("Welcome to our store. Browse our products and categories.")),
		blueprintResources["products"].WithColumns("name", "price", "status").WithLimit(8).WithHeading("Featured Products").WithEmpty("No products available yet.").List(ctx),
	)
}

type ProductsScreen struct{ component.ContextOnly }

func (s *ProductsScreen) ScreenTitle() string        { return "All Products" }
func (s *ProductsScreen) ScreenDescription() string  { return "Browse our full product catalog" }
func (s *ProductsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ProductsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Products")),
		blueprintResources["products"].WithColumns("name", "price", "status", "stock").WithLimit(20).WithHeading("Product Catalog").WithEmpty("No products found.").List(ctx),
	)
}

type CategoriesScreen struct{ component.ContextOnly }

func (s *CategoriesScreen) ScreenTitle() string        { return "Categories" }
func (s *CategoriesScreen) ScreenDescription() string  { return "Browse product categories" }
func (s *CategoriesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CategoriesScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Categories")),
		blueprintResources["categories"].WithColumns("name", "description", "active").WithLimit(50).WithHeading("All Categories").WithEmpty("No categories yet.").List(ctx),
	)
}

type OrdersScreen struct{ component.ContextOnly }

func (s *OrdersScreen) ScreenTitle() string        { return "Orders" }
func (s *OrdersScreen) ScreenDescription() string  { return "View and manage orders" }
func (s *OrdersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrdersScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Orders")),
		blueprintResources["orders"].WithColumns("order_number", "customer_name", "status", "total").WithLimit(20).WithHeading("Recent Orders").WithEmpty("No orders yet.").List(ctx),
	)
}

type ReviewsScreen struct{ component.ContextOnly }

func (s *ReviewsScreen) ScreenTitle() string        { return "Reviews" }
func (s *ReviewsScreen) ScreenDescription() string  { return "Customer reviews and ratings" }
func (s *ReviewsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ReviewsScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Customer Reviews")),
		blueprintResources["reviews"].WithColumns("author_name", "rating", "title").WithLimit(20).WithHeading("Latest Reviews").WithEmpty("No reviews yet.").List(ctx),
	)
}

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
		blueprintResources["products"].Detail(ctx, s.id),
	)
}

type OrderDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *OrderDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *OrderDetailScreen) ScreenTitle() string           { return "Order Details" }
func (s *OrderDetailScreen) ScreenDescription() string     { return "View order details" }
func (s *OrderDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *OrderDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Order Details")),
		blueprintResources["orders"].Detail(ctx, s.id),
	)
}

type ProductsEditScreen struct {
	component.ContextOnly
	id string
}

func (s *ProductsEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *ProductsEditScreen) ScreenTitle() string           { return "Edit Product" }
func (s *ProductsEditScreen) ScreenDescription() string     { return "" }
func (s *ProductsEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *ProductsEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["products"].Form(ctx, s.id),
	)
}

type OrdersEditScreen struct {
	component.ContextOnly
	id string
}

func (s *OrdersEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *OrdersEditScreen) ScreenTitle() string           { return "Edit Order" }
func (s *OrdersEditScreen) ScreenDescription() string     { return "" }
func (s *OrdersEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *OrdersEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		blueprintResources["orders"].Form(ctx, s.id),
	)
}
