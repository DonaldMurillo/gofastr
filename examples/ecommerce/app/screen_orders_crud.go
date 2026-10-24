//gofastr:generated generator=app kind=scaffold version=dev sum=4727e65414778ce9

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
	ordersCrudTextOrders              = i18n.Define("shopfront.screens.orders_crud.orders", "Orders")
	ordersCrudTextViewAndManageOrders = i18n.Define("shopfront.screens.orders_crud.view_and_manage_orders", "View and manage orders")
	ordersCrudTextRecentOrders        = i18n.Define("shopfront.screens.orders_crud.recent_orders", "Recent Orders")
	ordersCrudTextNoOrdersYet         = i18n.Define("shopfront.screens.orders_crud.no_orders_yet", "No orders yet.")
	ordersCrudTextOrderDetails        = i18n.Define("shopfront.screens.orders_crud.order_details", "Order Details")
	ordersCrudTextViewOrderDetails    = i18n.Define("shopfront.screens.orders_crud.view_order_details", "View order details")
)

type OrdersScreen struct{ component.ContextOnly }

func (s *OrdersScreen) ScreenTitle(ctx context.Context) string { return ordersCrudTextOrders.Text(ctx) }
func (s *OrdersScreen) ScreenDescription(ctx context.Context) string {
	return ordersCrudTextViewAndManageOrders.Text(ctx)
}
func (s *OrdersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrdersScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(ordersCrudTextOrders.Text(ctx))),
		appUI.List("orders").Columns("order_number", "customer_name", "status", "total").PageSize(20).NoCreate().Heading(ordersCrudTextRecentOrders.Text(ctx), 2).Empty(ordersCrudTextNoOrdersYet.Text(ctx)).RenderCtx(ctx),
	)
}

type OrderDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *OrderDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *OrderDetailScreen) ScreenTitle(ctx context.Context) string {
	return ordersCrudTextOrderDetails.Text(ctx)
}
func (s *OrderDetailScreen) ScreenDescription(ctx context.Context) string {
	return ordersCrudTextViewOrderDetails.Text(ctx)
}
func (s *OrderDetailScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OrderDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		html.Heading(html.HeadingConfig{Level: 1, Class: ""}, render.Text(ordersCrudTextOrderDetails.Text(ctx))),
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
