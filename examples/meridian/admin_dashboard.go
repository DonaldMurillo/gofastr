package main

import (
	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The admin's own words: the dashboard's figures and the theme it
// offers.
var (
	metricMRR             = i18n.Define("meridian.admin.metrics.mrr", "MRR")
	metricActiveCustomers = i18n.Define("meridian.admin.metrics.active_customers", "Active customers")
	metricTrialing        = i18n.Define("meridian.admin.metrics.trialing", "trialing")
	metricPastDue         = i18n.Define("meridian.admin.metrics.past_due_invoices", "Past-due invoices")
	metricOutstanding     = i18n.Define("meridian.admin.metrics.outstanding", "outstanding")
	themeBrutal           = i18n.Define("meridian.admin.themes.brutal", "Brutal")
)

// adminMetrics is the strip at the top of the admin dashboard: the
// figures a billing team opens the admin to check.
var adminMetrics = []admin.Metric{
	{Label: metricMRR, Entity: "customers", Agg: "sum", Field: "mrr", Where: `status = "active"`, Format: "money", View: "active"},
	{Label: metricActiveCustomers, Entity: "customers", Where: `status = "active"`, View: "active",
		Detail: &admin.Metric{Label: metricTrialing, Where: `status = "trialing"`}},
	{Label: metricPastDue, Entity: "invoices", Where: `status = "past_due"`, View: "past_due",
		Detail: &admin.Metric{Label: metricOutstanding, Agg: "sum", Field: "amount", Where: `status = "past_due"`, Format: "money", Tone: ui.TrendDown}},
}

// adminAttention is the Needs attention panel beside the recent
// activity: the rows someone has to chase.
var adminAttention = []admin.Watch{
	{Entity: "invoices", View: "past_due", Columns: []string{"number", "customer_id", "amount", "due_on"}},
	{Entity: "subscriptions", View: "past_due", Columns: []string{"customer_id", "plan_id", "mrr"}},
}

// brutalTheme is the admin's second look: the same tokens re-skinned
// with square corners, 2px strokes and hard shadows, picked from the
// toolbar and remembered per browser.
var brutalTheme = style.RegisterThemeOverride(theme.Brutal())

// adminThemes are the page themes the admin's toolbar offers.
var adminThemes = []ui.ThemeChoice{{Label: themeBrutal, Theme: brutalTheme}}
