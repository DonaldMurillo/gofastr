package main

import (
	"context"

	"database/sql"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The words this file says, in English. The app's catalog
// (locales/*.json) translates them by key.
var (
	invoicesCrudTextOverview          = i18n.Define("meridian.screens.invoices_crud.overview", "Overview")
	invoicesCrudTextYourRevenueAtA    = i18n.Define("meridian.screens.invoices_crud.your_revenue_at_a", "Your revenue at a glance.")
	invoicesCrudTextRevenueAtAGlance  = i18n.Define("meridian.screens.invoices_crud.revenue_at_a_glance", "Revenue at a glance")
	invoicesCrudTextMrr               = i18n.Define("meridian.screens.invoices_crud.mrr", "MRR")
	invoicesCrudTextActiveCustomers   = i18n.Define("meridian.screens.invoices_crud.active_customers", "Active customers")
	invoicesCrudTextPastDueInvoices   = i18n.Define("meridian.screens.invoices_crud.past_due_invoices", "Past-due invoices")
	invoicesCrudTextPlans             = i18n.Define("meridian.screens.invoices_crud.plans", "Plans")
	invoicesCrudTextCustomersByStatus = i18n.Define("meridian.screens.invoices_crud.customers_by_status", "Customers by status")
	invoicesCrudTextRecentInvoices    = i18n.Define("meridian.screens.invoices_crud.recent_invoices", "Recent invoices")
	invoicesCrudTextNoInvoicesYet     = i18n.Define("meridian.screens.invoices_crud.no_invoices_yet", "No invoices yet.")
	invoicesCrudTextInvoices          = i18n.Define("meridian.screens.invoices_crud.invoices", "Invoices")
	invoicesCrudTextInvoice           = i18n.Define("meridian.screens.invoices_crud.invoice", "Invoice")
	invoicesCrudTextNewInvoice        = i18n.Define("meridian.screens.invoices_crud.new_invoice", "New Invoice")
)

type DashboardScreen struct{ component.ContextOnly }

func (s *DashboardScreen) ScreenTitle(ctx context.Context) string {
	return invoicesCrudTextOverview.Text(ctx)
}
func (s *DashboardScreen) ScreenDescription(ctx context.Context) string {
	return invoicesCrudTextYourRevenueAtA.Text(ctx)
}
func (s *DashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		ui.PageHeader(ui.PageHeaderConfig{Title: invoicesCrudTextOverview.Text(ctx), Subtitle: invoicesCrudTextRevenueAtAGlance.Text(ctx), Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "12rem"}, ui.StatCard(ui.StatCardConfig{Label: invoicesCrudTextMrr.Text(ctx), Value: appUI.StatValue(ctx, "subscriptions", "sum", "mrr", `status = "active"`, "money")}), ui.StatCard(ui.StatCardConfig{Label: invoicesCrudTextActiveCustomers.Text(ctx), Value: appUI.StatValue(ctx, "customers", "count", "", `status = "active"`, "")}), ui.StatCard(ui.StatCardConfig{Label: invoicesCrudTextPastDueInvoices.Text(ctx), Value: appUI.StatValue(ctx, "invoices", "count", "", `status = "past_due"`, "")}), ui.StatCard(ui.StatCardConfig{Label: invoicesCrudTextPlans.Text(ctx), Value: appUI.StatValue(ctx, "plans", "count", "", "", "")})),
		ui.Card(ui.CardConfig{Heading: invoicesCrudTextCustomersByStatus.Text(ctx), HeadingLevel: 2}, ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: appUI.GroupBars(ctx, "customers", "status"), ShowLabels: true})),
		// Base pins the record links at the invoices list screen: this
		// list lives on /app, and its rows belong to /app/invoices/<id>.
		appUI.List("invoices").Columns("number", "customer_id", "amount", "status", "due_on").PageSize(8).NoCreate().Heading(invoicesCrudTextRecentInvoices.Text(ctx), 2).Empty(invoicesCrudTextNoInvoicesYet.Text(ctx)).Base("/app/invoices").RenderCtx(ctx),
	)
}

type InvoicesScreen struct{ component.ContextOnly }

func (s *InvoicesScreen) ScreenTitle(ctx context.Context) string {
	return invoicesCrudTextInvoices.Text(ctx)
}
func (s *InvoicesScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *InvoicesScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *InvoicesScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// Search over number and the status + customer facets come from
		// the entity itself (search_fields, display facets in
		// gofastr.yml), not per-screen config.
		appUI.List("invoices").Columns("number", "customer_id", "amount", "status", "issued_on", "due_on").PageSize(25).Bulk().Heading(invoicesCrudTextInvoices.Text(ctx), 1).Empty(invoicesCrudTextNoInvoicesYet.Text(ctx)).RenderCtx(ctx),
	)
}

type InvoiceDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *InvoiceDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *InvoiceDetailScreen) ScreenTitle(ctx context.Context) string {
	return invoicesCrudTextInvoice.Text(ctx)
}
func (s *InvoiceDetailScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *InvoiceDetailScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *InvoiceDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		// The record page holds the edit form; the Mark paid and Void
		// buttons come from the entity's States (gofastr.yml), including
		// their variants and the paid_on stamp. The invoice's payments
		// have no screen of their own, so they list without links.
		appUI.Record("invoices", s.id).RelatedAt("payments", "").Delete().Duplicate().RenderCtx(ctx),
	)
}

type InvoicesCreateScreen struct{ component.ContextOnly }

func (s *InvoicesCreateScreen) ScreenTitle(ctx context.Context) string {
	return invoicesCrudTextNewInvoice.Text(ctx)
}
func (s *InvoicesCreateScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *InvoicesCreateScreen) ScreenType() app.ScreenType                   { return app.ScreenPage }

func (s *InvoicesCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL},
		appUI.Create("invoices").Base("/app/invoices").RenderCtx(ctx),
	)
}

func mountDashboardScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app", &DashboardScreen{}).WithTitle("Overview").WithDescription("Your revenue at a glance.").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoicesScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices", &InvoicesScreen{}).WithTitle("Invoices").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoiceDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	record := app.NewScreen("/app/invoices/:id", &InvoiceDetailScreen{}).WithTitle("Invoice").WithPolicy(authPolicy("/login", ""))
	record.Intercept = &app.Intercept{From: "/app/invoices", As: app.ScreenDrawer}
	site.RegisterScreen(record, appLayout)
}

func mountInvoicesCreateScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices/create", &InvoicesCreateScreen{}).WithTitle("New Invoice").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 7, fn: mountDashboardScreen},
		screenRegistrar{order: 10, fn: mountInvoicesScreen},
		screenRegistrar{order: 11, fn: mountInvoiceDetailScreen},
		screenRegistrar{order: 15, fn: mountInvoicesCreateScreen},
	)
}
