package main

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// appExtensions holds the entityui extensions this app adds beside its
// screens: a view whose filter depends on the caller, a field kind, a
// record tab, a side panel or an action, keyed by entity. See 'gofastr
// docs blueprints' ("Extending the screens") and the framework/entityui
// package doc for each extension point.
var appExtensions = entityui.Extensions{
	Entities: map[string]entityui.Extension{
		"customers": {Side: []entityui.SidePanel{customerBilling}},
	},
}

// customerBilling is the customer record's Billing panel: what the
// customer owes, what they have paid and how many invoices they have,
// read as the caller, so a figure they may not see reads "—".
var customerBilling = entityui.SidePanel{
	Key: "billing", Title: "Billing",
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

func (p billingPanel) RenderCtx(ctx context.Context) render.HTML {
	mine := `customer_id = "` + dslEscape(p.customer) + `"`
	stat := func(agg, field, where, format string) render.HTML {
		return render.Text(p.ui.StatValue(ctx, "invoices", agg, field, mine+where, format))
	}
	return ui.DetailList(ui.DetailListConfig{Spread: true, Items: []ui.DetailItem{
		{Label: "Outstanding", Value: stat("sum", "amount", ` and status in ["open", "past_due"]`, "money")},
		{Label: "Paid to date", Value: stat("sum", "amount", ` and status = "paid"`, "money")},
		{Label: "Invoices", Value: stat("count", "", "", "")},
	}})
}

// dslEscape escapes a value for a double-quoted filter DSL string, whose
// only escapes are \" and \\.
func dslEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
