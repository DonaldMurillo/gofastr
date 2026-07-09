package main

import (
	"context"

	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
)

type CustomersScreen struct{ component.ContextOnly }

func (s *CustomersScreen) ScreenTitle() string        { return "Customers" }
func (s *CustomersScreen) ScreenDescription() string  { return "" }
func (s *CustomersScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		// customersList (app.go) is shared with the /api/tables/customers island
		// endpoint: the table renders in island mode, so sort + pagination RPC
		// and swap just the table.
		customersList().List(ctx),
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
	return render.Tag("div", nil,
		appResources["customers"].Detail(ctx, s.id),
	)
}

type CustomersNewScreen struct{ component.ContextOnly }

func (s *CustomersNewScreen) ScreenTitle() string        { return "New Customer" }
func (s *CustomersNewScreen) ScreenDescription() string  { return "" }
func (s *CustomersNewScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CustomersNewScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["customers"].Form(ctx, ""),
	)
}

type CustomersEditScreen struct {
	component.ContextOnly
	id string
}

func (s *CustomersEditScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *CustomersEditScreen) ScreenTitle() string           { return "Edit Customer" }
func (s *CustomersEditScreen) ScreenDescription() string     { return "" }
func (s *CustomersEditScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *CustomersEditScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.Tag("div", nil,
		appResources["customers"].Form(ctx, s.id),
	)
}

func mountCustomersScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["customers"] = ResourceConfig{
		Title: "Customers", Singular: "Customer", BasePath: "/app/customers", APIPath: "/api/customers",
		Crud:    fwApp.MustCrudHandler("customers"),
		CanEdit: true,
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "email", Label: "Email", Type: "string"},
			{Key: "company", Label: "Company", Type: "string"},
			{Key: "status", Label: "Status", Type: "enum", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Key: "mrr", Label: "MRR", Type: "decimal"},
		},
		Related: []RelatedList{
			{
				Title: "Invoices", ForeignKey: "customer_id", BasePath: "/app/invoices",
				Crud: fwApp.MustCrudHandler("invoices"),
				Fields: []ResField{
					{Key: "number", Label: "Number", Type: "string"},
					{Key: "amount", Label: "Amount", Type: "decimal"},
					{Key: "status", Label: "Status", Type: "enum"},
					{Key: "issued_on", Label: "Issued", Type: "date"},
				},
				Relations: map[string]RelSource{
					"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
				},
			},
			{
				Title: "Payments", ForeignKey: "customer_id", BasePath: "",
				Crud: fwApp.MustCrudHandler("payments"),
				Fields: []ResField{
					{Key: "invoice_id", Label: "Invoice", Type: "relation"},
					{Key: "amount", Label: "Amount", Type: "decimal"},
					{Key: "method", Label: "Method", Type: "enum"},
					{Key: "status", Label: "Status", Type: "enum"},
				},
				Relations: map[string]RelSource{
					"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
					"invoice_id":  {Crud: fwApp.MustCrudHandler("invoices"), Display: "number"},
				},
			},
			{
				Title: "Subscriptions", ForeignKey: "customer_id", BasePath: "/app/subscriptions",
				Crud: fwApp.MustCrudHandler("subscriptions"),
				Fields: []ResField{
					{Key: "plan_id", Label: "Plan", Type: "relation"},
					{Key: "status", Label: "Status", Type: "enum"},
					{Key: "mrr", Label: "MRR", Type: "decimal"},
					{Key: "started_on", Label: "Started", Type: "date"},
				},
				Relations: map[string]RelSource{
					"customer_id": {Crud: fwApp.MustCrudHandler("customers"), Display: "name"},
					"plan_id":     {Crud: fwApp.MustCrudHandler("plans"), Display: "name"},
				},
			},
		},
	}
	site.RegisterScreen(app.NewScreen("/app/customers", &CustomersScreen{}).WithTitle("Customers").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountCustomerDetailScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/customers/:id", &CustomerDetailScreen{}).WithTitle("Customer").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountCustomersNewScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/customers/new", &CustomersNewScreen{}).WithTitle("New Customer").WithPolicy(authPolicy("/login", "")), appLayout)
}

func mountCustomersEditScreen(fwApp *framework.App, site *app.App, db *sql.DB) {
	site.RegisterScreen(app.NewScreen("/app/customers/:id/edit", &CustomersEditScreen{}).WithTitle("Edit Customer").WithPolicy(authPolicy("/login", "")), appLayout)
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 8, fn: mountCustomersScreen},
		screenRegistrar{order: 9, fn: mountCustomerDetailScreen},
		screenRegistrar{order: 14, fn: mountCustomersNewScreen},
		screenRegistrar{order: 15, fn: mountCustomersEditScreen},
	)
}
