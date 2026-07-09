package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type DashboardScreen struct{ component.ContextOnly }

func (s *DashboardScreen) ScreenTitle() string        { return "Overview" }
func (s *DashboardScreen) ScreenDescription() string  { return "Your revenue at a glance." }
func (s *DashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{Title: "Overview", Subtitle: "Revenue at a glance", Eyebrow: ""}),
		ui.Grid(ui.GridConfig{Min: "12rem"}, ui.StatCard(ui.StatCardConfig{Label: "MRR", Value: statValue(ctx, "subscriptions", "sum", "mrr", "status=active", "money")}), ui.StatCard(ui.StatCardConfig{Label: "Active customers", Value: statValue(ctx, "customers", "count", "", "status=active", "")}), ui.StatCard(ui.StatCardConfig{Label: "Past-due invoices", Value: statValue(ctx, "invoices", "count", "", "status=past_due", "")}), ui.StatCard(ui.StatCardConfig{Label: "Plans", Value: statValue(ctx, "plans", "count", "", "", "")})),
		ui.Card(ui.CardConfig{Heading: "Customers by status", HeadingLevel: 2}, ui.BarChart(ui.BarChartConfig{Bars: groupBars(ctx, "customers", "status"), ShowLabels: true})),
		appResources["invoices"].WithColumns("number", "customer_id", "amount", "status", "due_on").WithLimit(8).WithHeading("Recent invoices").WithEmpty("No invoices yet.").List(ctx),
	)
}

type InvoicesScreen struct{ component.ContextOnly }

func (s *InvoicesScreen) ScreenTitle() string        { return "Invoices" }
func (s *InvoicesScreen) ScreenDescription() string  { return "" }
func (s *InvoicesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *InvoicesScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["invoices"].WithColumns("number", "customer_id", "amount", "status", "issued_on", "due_on").WithSearch("number").WithFilters(ResFilter{Key: "status", Label: "Status", Type: "enum", Values: []string{"draft", "open", "paid", "past_due", "void"}}, ResFilter{Key: "customer_id", Label: "Customer", Type: "relation"}).WithLimit(25).WithCreate().WithHeading("Invoices").WithEmpty("No invoices yet.").List(ctx),
	)
}

type InvoiceDetailScreen struct {
	component.ContextOnly
	id string
}

func (s *InvoiceDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *InvoiceDetailScreen) ScreenTitle() string           { return "Invoice" }
func (s *InvoiceDetailScreen) ScreenDescription() string     { return "" }
func (s *InvoiceDetailScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *InvoiceDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["invoices"].WithTransitions(Transition{Label: "Mark paid", Status: "paid", Variant: "primary", Stamp: "paid_on"}, Transition{Label: "Void", Status: "void", Variant: "danger", Stamp: ""}).Detail(ctx, s.id),
	)
}

type InvoicesNewScreen struct{ component.ContextOnly }

func (s *InvoicesNewScreen) ScreenTitle() string        { return "New Invoice" }
func (s *InvoicesNewScreen) ScreenDescription() string  { return "" }
func (s *InvoicesNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *InvoicesNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["invoices"].Form(ctx, ""),
	)
}

type InvoicesEditScreen struct {
	component.ContextOnly
	id string
}

func (s *InvoicesEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *InvoicesEditScreen) ScreenTitle() string           { return "Edit Invoice" }
func (s *InvoicesEditScreen) ScreenDescription() string     { return "" }
func (s *InvoicesEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *InvoicesEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["invoices"].Form(ctx, s.id),
	)
}

func mountDashboardScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["invoices"] = ResourceConfig{
		Title: "Invoices", Singular: "Invoice", BasePath: "/app/invoices", APIPath: "/api/invoices",
		Crud:    fwApp.MustCrudHandler("invoices"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "customer_id", Label: "Customer", Type: "relation"},
			{Key: "number", Label: "Number", Type: "string"},
			{Key: "amount", Label: "Amount", Type: "decimal"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"draft", "open", "paid", "past_due", "void"}},
			{Key: "issued_on", Label: "Issued", Type: "date"},
			{Key: "due_on", Label: "Due", Type: "date"},
			{Key: "paid_on", Label: "Paid", Type: "date"},
		},
		Relations: map[string]RelSource{
			"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
		},
		Related: []RelatedList{
			{
				Title: "Payments", ForeignKey: "invoice_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("payments"),
				Fields: []ResField{
					{Key: "customer_id", Label: "Customer", Type: "relation"},
					{Key: "amount", Label: "Amount", Type: "decimal"},
					{Key: "method", Label: "Method", Type: "enum"},
					{Key: "status", Label: "Status", Type: "enum"},
				},
				Relations: map[string]RelSource{
					"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
					"invoice_id":  {Crud: fwApp.MustCrudHandler("invoices"), Display: "number"},
				},
			},
		},
	}
	site.RegisterScreen(app.NewScreen("/app", &DashboardScreen{}).WithTitle("Overview").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoicesScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices", &InvoicesScreen{}).WithTitle("Invoices").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoiceDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices/:id", &InvoiceDetailScreen{}).WithTitle("Invoice").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoicesNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices/new", &InvoicesNewScreen{}).WithTitle("New Invoice").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountInvoicesEditScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/invoices/:id/edit", &InvoicesEditScreen{}).WithTitle("Edit Invoice").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 7, fn: mountDashboardScreen},
		screenRegistrar{order: 10, fn: mountInvoicesScreen},
		screenRegistrar{order: 11, fn: mountInvoiceDetailScreen},
		screenRegistrar{order: 16, fn: mountInvoicesNewScreen},
		screenRegistrar{order: 17, fn: mountInvoicesEditScreen},
	)
}
