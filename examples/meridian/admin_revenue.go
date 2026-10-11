package main

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Revenue report: an admin page of the app's own, built from kit
// pieces over the entity screens' stats. Every figure is read as the
// caller (admin.Page.Build never runs elevated), so a figure the
// caller may not see reads "—".

// revenuePage is the report's admin page, in the Billing group.
var revenuePage = admin.Page{
	Path:  "/revenue",
	Title: "Revenue",
	Nav:   &entity.EntityNav{Group: "billing", Icon: "chart", Order: 10},
	Build: func(r *http.Request) (component.Component, error) {
		return revenueReport{}, nil
	},
}

// revenueReport draws the figures and the breakdowns.
type revenueReport struct{ component.ContextOnly }

func (revenueReport) RenderCtx(ctx context.Context) render.HTML {
	stat := func(entityName, agg, field, where, format string) string {
		return appUI.StatValue(ctx, entityName, agg, field, where, format)
	}
	figures := ui.Grid(ui.GridConfig{Min: "14rem", Gap: ui.GapMD},
		ui.StatCard(ui.StatCardConfig{Label: "MRR", Value: stat("customers", "sum", "mrr", `status = "active"`, "money"), Icon: "activity"}),
		ui.StatCard(ui.StatCardConfig{Label: "Collected", Value: stat("payments", "sum", "amount", `status = "succeeded"`, "money"), Icon: "card"}),
		ui.StatCard(ui.StatCardConfig{Label: "Outstanding", Value: stat("invoices", "sum", "amount", `status in ["open", "past_due"]`, "money"), Icon: "receipt"}),
		ui.StatCard(ui.StatCardConfig{Label: "Failed payments", Value: stat("payments", "count", "", `status = "failed"`, ""), Icon: "warning"}),
	)
	breakdown := func(title, entityName, groupBy string) render.HTML {
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.BarChart(ui.BarChartConfig{Bars: appUI.GroupBars(ctx, entityName, groupBy), ShowLabels: true, FitHeight: true}))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Title: "Revenue", Subtitle: "What comes in, what is owed, and what failed."}),
		figures,
		ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapMD},
			breakdown("Invoices by status", "invoices", "status"),
			breakdown("Payments by method", "payments", "method"),
			breakdown("Subscriptions by status", "subscriptions", "status"),
		),
	)
}
