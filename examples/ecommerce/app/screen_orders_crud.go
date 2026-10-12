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

type OrdersScreen struct{ component.ContextOnly }

func (s *OrdersScreen) ScreenTitle() string        { return "Orders" }
func (s *OrdersScreen) ScreenDescription() string  { return "View and manage orders" }
func (s *OrdersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrdersScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Orders")),
		appUI.List("orders").Columns("order_number", "customer_name", "status", "total").PageSize(20).NoCreate().Heading("Recent Orders", 2).Empty("No orders yet.").RenderCtx(ctx),
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
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text("Order Details")),
		appUI.Record("orders", s.id).Delete().Duplicate().RenderCtx(ctx),
	)
}

func mountOrdersScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.Register("/orders", &OrdersScreen{}, appLayout)
}

func mountOrderDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	record := app.NewScreen("/orders/:id", &OrderDetailScreen{}).WithTitle("Order Details").WithDescription("View order details")
	record.Intercept = &app.Intercept{From: "/orders", As: app.ScreenDrawer}
	site.RegisterScreen(record, appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 3, fn: mountOrdersScreen},
		screenRegistrar{order: 7, fn: mountOrderDetailScreen},
	)
}
