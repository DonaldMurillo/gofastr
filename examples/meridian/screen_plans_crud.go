package main

import (
	"database/sql"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
)

// mountPlansResource wires the plans resource (no screen mounts it; it is looked up by
// data sources / relation labels elsewhere).
func mountPlansResource(fwApp *framework.App, site *app.App, db *sql.DB) {
	appResources["plans"] = ResourceConfig{
		Title: "Plans", Singular: "Plan", BasePath: "", APIPath: "/api/plans",
		Crud: fwApp.MustCrudHandler("plans"),
		Fields: []ResField{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "slug", Label: "Slug", Type: "string"},
			{Key: "price", Label: "Price", Type: "decimal"},
			{Key: "interval", Label: "Interval", Type: "enum", Values: []string{"month", "year"}},
			{Key: "active", Label: "Active", Type: "bool"},
		},
		Related: []RelatedList{
			{
				Title: "Subscriptions", ForeignKey: "plan_id", BasePath: "/app/subscriptions",
				Crud: fwApp.MustCrudHandler("subscriptions"),
				Fields: []ResField{
					{Key: "customer_id", Label: "Customer", Type: "relation"},
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
}

func init() {
	screenRegistrars = append(screenRegistrars,
		screenRegistrar{order: 0, fn: mountPlansResource},
	)
}
