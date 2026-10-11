package main

import (
	"context"

	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type CustomersScreen struct{ component.ContextOnly }

func (s *CustomersScreen) ScreenTitle() string        { return "Customers" }
func (s *CustomersScreen) ScreenDescription() string  { return "" }
func (s *CustomersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// The list keeps its sort, page, search and status facet in the
		// page's own query string: links and the toolbar GET form
		// navigate, the runtime swaps the screen partial. Search and the
		// facet come from the entity itself (search_fields, display
		// facets in gofastr.yml), not per-screen config. Quick add is
		// the app's own action beside New: it opens the quick-add modal.
		appUI.List("customers").Columns("name", "email", "company", "status", "mrr").PageSize(8).Heading("Customers", 1).Empty("No customers yet. Add your first to get started.").
			Actions(interactive.OpenOnClick(ui.Button(ui.ButtonConfig{Label: "Quick add", Variant: ui.ButtonSecondary}), "customer-quick-add")).
			RenderCtx(ctx),
	)
}

type CustomerDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *CustomerDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *CustomerDetailScreen) ScreenTitle() string           { return "Customer" }
func (s *CustomerDetailScreen) ScreenDescription() string     { return "" }
func (s *CustomerDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *CustomerDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// The record page holds the edit form; the move buttons would come
		// from the entity's States (customers has none). Related lists
		// invoices and subscriptions, which link to their own screens;
		// payments has no screen, so its rows draw without links.
		appUI.Record("customers", s.id).Related("invoices", "subscriptions").RelatedAt("payments", "").
			Delete().Duplicate().RenderCtx(ctx),
	)
}

type CustomersCreateScreen struct{ component.ContextOnly }

func (s *CustomersCreateScreen) ScreenTitle() string        { return "New Customer" }
func (s *CustomersCreateScreen) ScreenDescription() string  { return "" }
func (s *CustomersCreateScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		appUI.Create("customers").Base("/app/customers").RenderCtx(ctx),
	)
}

func mountCustomersScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/customers", &CustomersScreen{}).WithTitle("Customers").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountCustomerDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	record := app.NewScreen("/app/customers/:id", &CustomerDetailScreen{}).WithTitle("Customer").WithPolicy(authPolicy("/login", ""))
	record.Intercept = &app.Intercept{From: "/app/customers", As: app.ScreenDrawer}
	site.RegisterScreen(record, appLayout)
}

func mountCustomersCreateScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/customers/create", &CustomersCreateScreen{}).WithTitle("New Customer").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 8, fn: mountCustomersScreen},
		screenRegistrar{order: 9, fn: mountCustomerDetailScreen},
		screenRegistrar{order: 14, fn: mountCustomersCreateScreen},
	)
}
