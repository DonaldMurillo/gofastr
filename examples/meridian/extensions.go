package main

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// appExtensions holds the entityui extensions this app adds beside its
// screens: a view whose filter depends on the caller, a field kind, a
// record tab, a side panel or an action, keyed by entity. See 'gofastr
// docs app-spec' ("Extending the screens") and the framework/entityui
// package doc for each extension point.
var appExtensions = entityui.Extensions{
	Entities: map[string]entityui.Extension{
		"customers": {Side: []entityui.SidePanel{customerBilling}},
	},
}

// The Billing panel's words: its title and the figures it lists.
var (
	panelBilling       = i18n.Define("meridian.customers.billing.title", "Billing")
	billingOutstanding = i18n.Define("meridian.customers.billing.outstanding", "Outstanding")
	billingPaidToDate  = i18n.Define("meridian.customers.billing.paid_to_date", "Paid to date")
	billingInvoices    = i18n.Define("meridian.customers.billing.invoices", "Invoices")
)

// customerBilling is the customer record's Billing panel: what the
// customer owes, what they have paid and how many invoices they have,
// read as the caller, so a figure they may not see reads "—".
var customerBilling = entityui.SidePanel{
	Key: "billing", Title: panelBilling,
	Build: func(rc entityui.RecordContext) (component.Component, error) {
		return billingPanel{ui: rc.UI, customer: rc.Record.ID}, nil
	},
}

// billingPanel draws one customer's invoice figures.
type billingPanel struct {
	component.ContextOnly
	ui       *entityui.UI
	customer string
}

// billingFigures are the Billing panel's figures, each filtered to the
// record's customer by billingWhere: RenderCtx draws them and
// checkBillingPanel checks them once at boot.
var billingFigures = []figureStat{
	{label: billingOutstanding, entity: "invoices", agg: "sum", field: "amount", where: `status in ["open", "past_due"]`, format: "money"},
	{label: billingPaidToDate, entity: "invoices", agg: "sum", field: "amount", where: `status = "paid"`, format: "money"},
	{label: billingInvoices, entity: "invoices", agg: "count"},
}

// billingWhere narrows a figure's filter to one customer's invoices.
func billingWhere(customer, where string) string {
	mine := `customer_id = "` + dslEscape(customer) + `"`
	if where == "" {
		return mine
	}
	return mine + " and " + where
}

// checkBillingPanel reports the first Billing figure that could never
// compute, filtered to a placeholder customer: the filter is checked
// against the entity's fields, not run.
func checkBillingPanel(u *entityui.UI) error {
	stats := make([]figureStat, len(billingFigures))
	for i, f := range billingFigures {
		f.where = billingWhere("customer", f.where)
		stats[i] = f
	}
	return checkFigures(u, `customers side panel "billing"`, stats, nil)
}

func (p billingPanel) RenderCtx(ctx context.Context) render.HTML {
	items := make([]ui.DetailItem, 0, len(billingFigures))
	for _, f := range billingFigures {
		f.where = billingWhere(p.customer, f.where)
		items = append(items, ui.DetailItem{Label: f.label.Text(ctx), Value: render.Text(f.value(ctx, p.ui))})
	}
	return ui.DetailList(ui.DetailListConfig{Spread: true, Items: items})
}

// dslEscape escapes a value for a double-quoted filter DSL string, whose
// only escapes are \" and \\.
func dslEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
