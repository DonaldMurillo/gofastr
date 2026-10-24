package main

import (
	"context"
	"math"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The Revenue report: an admin page of the app's own, built from kit
// pieces over the entity screens' stats. Every figure is read as the
// caller (admin.Page.Build never runs elevated), so a figure the
// caller may not see reads "—".

// The report's words. revenueTitle is its heading, crumb and nav entry.
var (
	revenueTitle                 = i18n.Define("meridian.admin.revenue.title", "Revenue")
	revenueSubtitle              = i18n.Define("meridian.admin.revenue.subtitle", "What comes in, what is owed, and what failed.")
	revenueCollected             = i18n.Define("meridian.admin.revenue.collected", "Collected")
	revenueOutstanding           = i18n.Define("meridian.admin.revenue.outstanding", "Outstanding")
	revenueFailed                = i18n.Define("meridian.admin.revenue.failed_payments", "Failed payments")
	revenuePaymentSuccess        = i18n.Define("meridian.admin.revenue.payment_success", "Payment success")
	revenueWentThrough           = i18n.Define("meridian.admin.revenue.went_through", "Payments that went through")
	revenueInvoicesByStatus      = i18n.Define("meridian.admin.revenue.invoices_by_status", "Invoices by status")
	revenuePaymentsByMethod      = i18n.Define("meridian.admin.revenue.payments_by_method", "Payments by method")
	revenueSubscriptionsByStatus = i18n.Define("meridian.admin.revenue.subscriptions_by_status", "Subscriptions by status")
	revenuePaymentsGrid          = i18n.Define("meridian.admin.revenue.payments_grid", "Payments by method and status")
)

// valueLabels names each of an enum field's values the way the entity
// screens do: the catalog's entity.<entity>.fields.<field>.values.<v>.
func valueLabels(ctx context.Context, entityName, field string, values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = i18nui.FieldValueLabel(ctx, nil, entityName, field, v)
	}
	return out
}

// revenuePage is the report's admin page, in the Billing group.
var revenuePage = admin.Page{
	Path:  "/revenue",
	Title: revenueTitle,
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
		ui.StatCard(ui.StatCardConfig{Label: metricMRR.Text(ctx), Value: stat("customers", "sum", "mrr", `status = "active"`, "money"), Icon: "activity"}),
		ui.StatCard(ui.StatCardConfig{Label: revenueCollected.Text(ctx), Value: stat("payments", "sum", "amount", `status = "succeeded"`, "money"), Icon: "card"}),
		ui.StatCard(ui.StatCardConfig{Label: revenueOutstanding.Text(ctx), Value: stat("invoices", "sum", "amount", `status in ["open", "past_due"]`, "money"), Icon: "receipt"}),
		ui.StatCard(ui.StatCardConfig{Label: revenueFailed.Text(ctx), Value: stat("payments", "count", "", `status = "failed"`, ""), Icon: "warning"}),
	)
	// Each chart is named by its card's heading, "<card ID>-title".
	breakdown := func(title i18n.Phrase, entityName, groupBy string) render.HTML {
		id := entityName + "-by-" + groupBy
		return ui.Card(ui.CardConfig{ID: id, Heading: title.Text(ctx), HeadingLevel: 2},
			ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: appUI.GroupBars(ctx, entityName, groupBy), ShowLabels: true, FitHeight: true, LabelledBy: id + "-title"}))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Title: revenueTitle.Text(ctx), Subtitle: revenueSubtitle.Text(ctx)}),
		figures,
		ui.Grid(ui.GridConfig{Min: "24rem", Gap: ui.GapMD},
			ui.Card(ui.CardConfig{Heading: revenuePaymentSuccess.Text(ctx), HeadingLevel: 2}, ui.Center(ui.CenterConfig{}, paymentSuccessGauge(ctx))),
			breakdown(revenueInvoicesByStatus, "invoices", "status"),
			breakdown(revenuePaymentsByMethod, "payments", "method"),
			breakdown(revenueSubscriptionsByStatus, "subscriptions", "status"),
		),
		ui.Card(ui.CardConfig{ID: "payments-grid", Heading: revenuePaymentsGrid.Text(ctx), HeadingLevel: 2}, paymentsHeatmap(ctx, "payments-grid-title")),
	)
}

// paymentsHeatmap counts payments for each method and status: where the
// failures cluster. A count the caller may not read is an empty cell.
func paymentsHeatmap(ctx context.Context, labelledBy string) render.HTML {
	methods := []string{"card", "ach", "wire"}
	statuses := []string{"succeeded", "failed", "refunded"}
	values := make([][]float64, len(methods))
	for i, m := range methods {
		values[i] = make([]float64, len(statuses))
		for j, s := range statuses {
			values[i][j] = math.NaN()
			if n, ok := appUI.CountInt(ctx, "payments", `method = "`+m+`" and status = "`+s+`"`); ok {
				values[i][j] = float64(n)
			}
		}
	}
	return ui.Heatmap(ui.HeatmapConfig{
		Ctx:        ctx,
		Rows:       valueLabels(ctx, "payments", "method", methods),
		Cols:       valueLabels(ctx, "payments", "status", statuses),
		Values:     values,
		Width:      320,
		ShowLegend: true,
		LabelledBy: labelledBy,
	})
}

// paymentSuccessGauge draws the share of payments that went through
// (succeeded, or succeeded and later refunded) out of every attempt. No
// payments yet, or a count the caller may not read, is no reading, and
// the gauge shows its empty state.
func paymentSuccessGauge(ctx context.Context) render.HTML {
	rate := math.NaN()
	all, okAll := appUI.CountInt(ctx, "payments", "")
	failed, okFailed := appUI.CountInt(ctx, "payments", `status = "failed"`)
	if okAll && okFailed && all > 0 {
		// Two reads, not one: a payment that fails between them can make
		// failed outrun all, and the share must not go below zero.
		rate = 100 * float64(all-min(failed, all)) / float64(all)
	}
	return ui.Gauge(ui.GaugeConfig{
		Ctx:       ctx,
		Label:     revenueWentThrough.Text(ctx),
		Size:      200,
		Value:     rate,
		ValueText: i18n.Percent(ctx, rate/100, i18n.MinFraction(1)),
		Bands: []ui.GaugeBand{
			{From: 0, Tone: ui.GaugeDanger},
			{From: 90, Tone: ui.GaugeWarning},
			{From: 97, Tone: ui.GaugeSuccess},
		},
	})
}
