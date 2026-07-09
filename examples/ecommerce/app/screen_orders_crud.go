package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type OrdersScreen struct{ component.ContextOnly }

func (s *OrdersScreen) ScreenTitle() string        { return "Orders" }
func (s *OrdersScreen) ScreenDescription() string  { return "View and manage orders" }
func (s *OrdersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrdersScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Orders")),
		appResources["orders"].WithColumns("order_number", "customer_name", "status", "total").WithLimit(20).WithHeading("Recent Orders").WithEmpty("No orders yet.").List(ctx),
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
		appResources["orders"].Detail(ctx, s.id),
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
		appResources["orders"].Form(ctx, s.id),
	)
}

func mountOrdersScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["orders"] = ResourceConfig{
		Title: "Orders", Singular: "Order", BasePath: "/orders", APIPath: "/api/orders",
		Crud:    fwApp.MustCrudHandler("orders"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "user_id", Label: "User", Type: "string"},
			{Key: "order_number", Label: "Order Number", Type: "string"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"pending", "confirmed", "processing", "shipped", "delivered", "cancelled", "refunded"}},
			{Key: "customer_name", Label: "Customer Name", Type: "string"},
			{Key: "customer_email", Label: "Customer Email", Type: "string"},
			{Key: "customer_phone", Label: "Customer Phone", Type: "string"},
			{Key: "shipping_address", Label: "Shipping Address", Type: "json"},
			{Key: "billing_address", Label: "Billing Address", Type: "json"},
			{Key: "subtotal", Label: "Subtotal", Type: "decimal"},
			{Key: "tax", Label: "Tax", Type: "decimal"},
			{Key: "shipping_cost", Label: "Shipping Cost", Type: "decimal"},
			{Key: "total", Label: "Total", Type: "decimal"},
			{Key: "notes", Label: "Notes", Type: "text"},
			{Key: "shipped_at", Label: "Shipped At", Type: "timestamp"},
			{Key: "delivered_at", Label: "Delivered At", Type: "timestamp"},
		},
		Related: []RelatedList{
			{
				Title: "Order Items", ForeignKey: "order_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("order_items"),
				Fields: []ResField{
					{Key: "user_id", Label: "User", Type: "string"},
					{Key: "product_id", Label: "Product", Type: "relation"},
					{Key: "product_name", Label: "Product Name", Type: "string"},
					{Key: "quantity", Label: "Quantity", Type: "int"},
				},
				Relations: map[string]RelSource{
					"order_id":   {Crud: fwApp.MustCrudHandler("orders"), Display: "user_id"},
					"product_id": {Crud: fwApp.MustCrudHandler("products"), Display: "name"},
				},
			},
		},
	}
	site.Register("/orders", &OrdersScreen{}, appLayout)
}

func mountOrderDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/order-detail", &OrderDetailScreen{}, appLayout)
}

func mountOrdersEditScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/order-detail/edit", &OrdersEditScreen{}, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 3, fn: mountOrdersScreen},
		screenRegistrar{order: 7, fn: mountOrderDetailScreen},
		screenRegistrar{order: 9, fn: mountOrdersEditScreen},
	)
}
