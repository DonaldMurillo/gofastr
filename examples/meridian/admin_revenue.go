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
	"github.com/DonaldMurillo/gofastr/framework/entityui"
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

// revenueStats and revenueCharts are the report's figures and
// breakdowns; paymentMethods and paymentStatuses are the heatmap's rows
// and columns. RenderCtx draws them all and checkRevenueReport checks
// them once at boot.
var (
	revenueStats = []figureStat{
		{label: metricMRR, icon: "activity", entity: "customers", agg: "sum", field: "mrr", where: `status = "active"`, format: "money"},
		{label: revenueCollected, icon: "card", entity: "payments", agg: "sum", field: "amount", where: `status = "succeeded"`, format: "money"},
		{label: revenueOutstanding, icon: "receipt", entity: "invoices", agg: "sum", field: "amount", where: `status in ["open", "past_due"]`, format: "money"},
		{label: revenueFailed, icon: "warning", entity: "payments", agg: "count", where: paymentsFailedWhere},
	}
	revenueCharts = []figureChart{
		{title: revenueInvoicesByStatus, entity: "invoices", groupBy: "status"},
		{title: revenuePaymentsByMethod, entity: "payments", groupBy: "method"},
		{title: revenueSubscriptionsByStatus, entity: "subscriptions", groupBy: "status"},
	}
	paymentMethods  = []string{"card", "ach", "wire"}
	paymentStatuses = []string{"succeeded", "failed", "refunded"}
)

// paymentsFailedWhere is the failed payments: a stat, and the gauge's
// share of attempts that did not go through.
const paymentsFailedWhere = `status = "failed"`

// paymentCellWhere is the heatmap cell for one method and status.
func paymentCellWhere(method, status string) string {
	return `method = "` + method + `" and status = "` + status + `"`
}

// checkRevenueReport reports the first of the report's figures, the
// gauge's and the heatmap's counts among them, that could never compute.
func checkRevenueReport(u *entityui.UI) error {
	stats := append([]figureStat{
		{label: revenuePaymentSuccess, entity: "payments"},
		{label: revenuePaymentSuccess, entity: "payments", where: paymentsFailedWhere},
	}, revenueStats...)
	for _, m := range paymentMethods {
		for _, s := range paymentStatuses {
			stats = append(stats, figureStat{label: revenuePaymentsGrid, entity: "payments", where: paymentCellWhere(m, s)})
		}
	}
	return checkFigures(u, `admin page "revenue"`, stats, revenueCharts)
}

// revenueReport draws the figures and the breakdowns.
type revenueReport struct{ component.ContextOnly }

func (revenueReport) RenderCtx(ctx context.Context) render.HTML {
	cards := make([]render.HTML, 0, len(revenueStats))
	for _, st := range revenueStats {
		cards = append(cards, ui.StatCard(ui.StatCardConfig{Label: st.label.Text(ctx), Value: st.value(ctx, appUI), Icon: st.icon}))
	}
	figures := ui.Grid(ui.GridConfig{Min: "14rem", Gap: ui.GapMD}, cards...)
	// The gauge, then one card per breakdown. Each chart is named by its
	// card's heading, "<card ID>-title".
	panels := []render.HTML{ui.Card(ui.CardConfig{Heading: revenuePaymentSuccess.Text(ctx), HeadingLevel: 2}, ui.Center(ui.CenterConfig{}, paymentSuccessGauge(ctx)))}
	for _, c := range revenueCharts {
		id := c.entity + "-by-" + c.groupBy
		panels = append(panels, ui.Card(ui.CardConfig{ID: id, Heading: c.title.Text(ctx), HeadingLevel: 2},
			ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: appUI.GroupBars(ctx, c.entity, c.groupBy), ShowLabels: true, FitHeight: true, LabelledBy: id + "-title"})))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{Title: revenueTitle.Text(ctx), Subtitle: revenueSubtitle.Text(ctx)}),
		figures,
		ui.Grid(ui.GridConfig{Min: "24rem", Gap: ui.GapMD}, panels...),
		ui.Card(ui.CardConfig{ID: "payments-grid", Heading: revenuePaymentsGrid.Text(ctx), HeadingLevel: 2}, paymentsHeatmap(ctx, "payments-grid-title")),
	)
}

// paymentsHeatmap counts payments for each method and status: where the
// failures cluster. A count the caller may not read is an empty cell.
func paymentsHeatmap(ctx context.Context, labelledBy string) render.HTML {
	methods, statuses := paymentMethods, paymentStatuses
	values := make([][]float64, len(methods))
	for i, m := range methods {
		values[i] = make([]float64, len(statuses))
		for j, s := range statuses {
			values[i][j] = math.NaN()
			if n, ok := appUI.CountInt(ctx, "payments", paymentCellWhere(m, s)); ok {
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
	failed, okFailed := appUI.CountInt(ctx, "payments", paymentsFailedWhere)
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
