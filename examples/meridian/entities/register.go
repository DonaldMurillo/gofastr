package entities

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }

// RegisterAll registers every generated entity declaration with app.
func RegisterAll(app *framework.App) {
	app.Entity("plans", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(80)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Max: floatPtr(80)},
			{Name: "price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "interval", Type: schema.Enum, Default: "month", Values: []string{"month", "year"}},
			{Name: "active", Type: schema.Bool, Default: true},
		},
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Plans"},
	})
	_ = Plans{}
	app.Entity("customers", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(120)},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
			{Name: "company", Type: schema.String, Max: floatPtr(120)},
			{Name: "status", Type: schema.Enum, Default: "trialing", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Name: "mrr", Type: schema.Decimal, Default: "0", Min: floatPtr(0)},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Indices: []framework.Index{
			{Name: "idx_customers_email", Columns: []string{"email"}, Unique: true},
		},
		Properties: map[string]any{"label": "Customers"},
	})
	_ = Customers{}
	app.Entity("subscriptions", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "plan_id", Type: schema.Relation, Required: true, To: "plans"},
			{Name: "status", Type: schema.Enum, Default: "trialing", Values: []string{"trialing", "active", "past_due", "canceled"}},
			{Name: "mrr", Type: schema.Decimal, Default: "0", Min: floatPtr(0)},
			{Name: "started_on", Type: schema.Date},
			{Name: "renews_on", Type: schema.Date},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
			{Type: framework.RelManyToOne, Name: "plan", Entity: "plans", ForeignKey: "plan_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Subscriptions"},
	})
	_ = Subscriptions{}
	app.Entity("invoices", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "number", Type: schema.String, Required: true, Unique: true},
			{Name: "amount", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "past_due", "void"}},
			{Name: "issued_on", Type: schema.Date},
			{Name: "due_on", Type: schema.Date},
			{Name: "paid_on", Type: schema.Date},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Invoices"},
	})
	_ = Invoices{}
	app.Entity("payments", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "invoice_id", Type: schema.Relation, Required: true, To: "invoices"},
			{Name: "customer_id", Type: schema.Relation, Required: true, To: "customers"},
			{Name: "amount", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "method", Type: schema.Enum, Default: "card", Values: []string{"card", "ach", "wire"}},
			{Name: "status", Type: schema.Enum, Default: "succeeded", Values: []string{"succeeded", "failed", "refunded"}},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "invoice", Entity: "invoices", ForeignKey: "invoice_id"},
			{Type: framework.RelManyToOne, Name: "customer", Entity: "customers", ForeignKey: "customer_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"label": "Payments"},
	})
	_ = Payments{}
}
