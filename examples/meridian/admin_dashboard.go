package main

import "github.com/DonaldMurillo/gofastr/battery/admin"

// adminMetrics is the strip at the top of the admin dashboard: the
// figures a billing team opens the admin to check.
var adminMetrics = []admin.Metric{
	{Label: "MRR", Entity: "customers", Agg: "sum", Field: "mrr", Where: `status = "active"`, Format: "money", View: "active"},
	{Label: "Active customers", Entity: "customers", Where: `status = "active"`, View: "active",
		Detail: &admin.Metric{Label: "trialing", Where: `status = "trialing"`}},
	{Label: "Past-due invoices", Entity: "invoices", Where: `status = "past_due"`, View: "past_due",
		Detail: &admin.Metric{Label: "outstanding", Agg: "sum", Field: "amount", Where: `status = "past_due"`, Format: "money"}},
}

// adminAttention is the Needs attention panel beside the recent
// activity: the rows someone has to chase.
var adminAttention = []admin.Watch{
	{Entity: "invoices", View: "past_due", Columns: []string{"number", "customer_id", "amount", "due_on"}},
	{Entity: "subscriptions", View: "past_due", Columns: []string{"customer_id", "plan_id", "mrr"}},
}
