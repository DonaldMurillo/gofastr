package telemetryui

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// routesShown is the rows of the overview's route table.
const routesShown = 50

// liveStepMax is the widest bar of a range that refreshes itself.
const liveStepMax = 15 * time.Minute

// span is one time range of the overview, and the width of one bar in
// it: about sixty to a hundred bars whatever the range.
type span struct {
	window
	step time.Duration
	bar  i18n.Phrase
}

var spans = []span{
	{windows[0], time.Minute, i18nui.TelemetryBarMinute},
	{windows[1], 15 * time.Minute, i18nui.TelemetryBarQuarter},
	{windows[2], 2 * time.Hour, i18nui.TelemetryBarTwoHours},
}

// pickSpan returns the chosen range, the last 24 hours when none or an
// unknown one is named. The overview has no "everything kept": its
// bars need a width.
func pickSpan(q url.Values) span {
	key := param(q, "window")
	for _, sp := range spans {
		if sp.key == key {
			return sp
		}
	}
	return spans[1]
}

// OverviewPath is where the host serves the overview page.
func (s *Screens) OverviewPath() string { return s.base + "/overview" }

// Overview draws the counts of every request in a time range: totals,
// how they moved over the range, and each route's share.
func (s *Screens) Overview(ctx context.Context) render.HTML { return s.overview(ctx, false) }

// OverviewRows draws the overview's figures alone: what the page
// re-fetches on its timer.
func (s *Screens) OverviewRows(ctx context.Context) render.HTML { return s.overview(ctx, true) }

func (s *Screens) overview(ctx context.Context, rowsOnly bool) render.HTML {
	sp := pickSpan(query(ctx))
	// A range whose bars are hours wide gains nothing from a refresh
	// every ten seconds, and it is the widest read: seven days of
	// counts, a third of a second of one core. It stays as loaded.
	live := s.Live != nil && sp.step <= liveStepMax
	header := ui.PageHeaderConfig{
		Title:    i18nui.TelemetryOverview.Text(ctx),
		Subtitle: i18nui.TelemetryOverviewSub.Text(ctx),
	}
	if live {
		header.Actions = s.liveBadge(ctx, cursor{})
	}
	store := s.tel.Reader()
	if store == nil {
		if rowsOnly {
			return ""
		}
		return page(s.tabNav(ctx, tabOverview), ui.PageHeader(header), problem(ctx, i18nui.TelemetryNotReady))
	}
	now := s.now()
	stats, err := store.Stats(itel.StatsQuery{From: now.Add(-sp.span), To: now, Step: sp.step})
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: read request counts", "err", err)
		if rowsOnly {
			return ""
		}
		return page(s.tabNav(ctx, tabOverview), ui.PageHeader(header), problem(ctx, i18nui.TelemetryLoadFailed))
	}
	wkey := sp.key
	if wkey == defaultWindow {
		wkey = ""
	}
	var body render.HTML
	if stats.Total.N == 0 {
		body = ui.EmptyState(ui.EmptyStateConfig{
			Title:       i18nui.TelemetryOverviewEmpty.Text(ctx),
			Description: i18nui.TelemetryOverviewEmptySub.Text(ctx),
		})
	} else {
		body = page(s.totals(ctx, stats.Total, wkey), s.charts(ctx, sp, stats.Points, now), s.routeTable(ctx, stats.Routes, wkey))
	}
	if rowsOnly {
		return body
	}
	facet := ui.Facet{Name: "window", Label: i18nui.TelemetryWindow.Text(ctx), Value: sp.key}
	for _, o := range spans {
		facet.Options = append(facet.Options, ui.FacetOption{Label: o.label.Text(ctx), Value: o.key})
	}
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: s.OverviewPath(), ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Facets:     []ui.Facet{facet},
		HideReset:  true,
	})
	if live {
		body = s.rows("overview", cursor{}, link(s.Live("overview"), "window", wkey), body)
	}
	return page(s.tabNav(ctx, tabOverview), ui.PageHeader(header), s.firing(ctx), filters, body)
}

// share writes part of whole as a percentage: one decimal under ten
// percent, where the difference between 0.2% and 0.9% is the news.
func share(ctx context.Context, part, whole int64) string {
	if whole <= 0 {
		return i18n.Percent(ctx, 0)
	}
	f := float64(part) / float64(whole)
	switch {
	case part > 0 && f < 0.001:
		// Three in six thousand is not "0%".
		return i18n.Percent(ctx, max(f, 0.0001), i18n.MaxFraction(2))
	case part > 0 && f < 0.1:
		return i18n.Percent(ctx, f, i18n.MaxFraction(1))
	}
	return i18n.Percent(ctx, f)
}

// within writes a percentile. One at the last bucket's edge is that
// long or longer: the counts do not say how much longer.
func within(ctx context.Context, d time.Duration) string {
	text := ui.DurationText(ctx, twoDigits(d))
	if d >= itel.StatsCeiling {
		return i18nui.TelemetryOrMore.Text(ctx, map[string]any{"duration": text})
	}
	return text
}

// twoDigits rounds d to two significant digits: a percentile read from
// buckets is right to within its bucket, and "683.5 ms" says more than
// the counts know.
func twoDigits(d time.Duration) time.Duration {
	unit := time.Duration(1)
	for d/unit >= 100 {
		unit *= 10
	}
	return (d + unit/2) / unit * unit
}

func (s *Screens) totals(ctx context.Context, t itel.StatsCount, wkey string) render.HTML {
	ofAll := func(n int64) string {
		return i18nui.TelemetryStatShare.Text(ctx, map[string]any{"share": share(ctx, n, t.N)})
	}
	failed := ui.StatCardConfig{Label: i18nui.TelemetryStatFailed.Text(ctx), Value: i18n.Number(ctx, t.Failed), Trend: ofAll(t.Failed), Plain: true,
		Href: link(s.RequestsPath(), "show", "failed", "window", wkey)}
	if t.Failed > 0 {
		failed.Direction = ui.TrendDown
	}
	return ui.StatStrip(ui.StatStripConfig{Label: i18nui.TelemetryStats.Text(ctx), Cells: []render.HTML{
		ui.StatCard(ui.StatCardConfig{Label: i18nui.TelemetryChartRequests.Text(ctx), Value: i18n.Number(ctx, t.N), Plain: true}),
		ui.StatCard(failed),
		ui.StatCard(ui.StatCardConfig{Label: i18nui.TelemetryStatSlow.Text(ctx), Value: i18n.Number(ctx, t.Slow), Trend: ofAll(t.Slow), Plain: true,
			Href: link(s.RequestsPath(), "show", "slow", "window", wkey)}),
		ui.StatCard(ui.StatCardConfig{Label: i18nui.TelemetryStatP95.Text(ctx), Value: within(ctx, t.P95), Plain: true}),
	}})
}

// charts draws three questions over the range: how busy, when did it
// fail, when was it slow.
func (s *Screens) charts(ctx context.Context, sp span, points []itel.StatsPoint, now time.Time) render.HTML {
	requests := make([]ui.BarChartBar, len(points))
	failed := make([]ui.BarChartBar, len(points))
	slowest := make([]ui.BarChartBar, len(points))
	for i, p := range points {
		at := i18n.DateTime(ctx, p.Time.UTC(), i18n.DateMedium, i18n.TimeShort)
		requests[i] = ui.BarChartBar{Label: at, Value: float64(p.N)}
		failed[i] = ui.BarChartBar{Label: at, Value: float64(p.Failed), Color: "danger"}
		slowest[i] = ui.BarChartBar{Label: at, Value: float64(p.P95.Microseconds()) / 1000, Color: "warning"}
	}
	// Where the bars start and end, to the minute: the first bar's
	// start carries its date when that is not today.
	end := func(t time.Time) render.HTML {
		u := t.UTC()
		text := i18n.Time(ctx, u, i18n.TimeShort)
		if n := now.UTC(); u.Year() != n.Year() || u.YearDay() != n.YearDay() {
			text = i18n.DateTime(ctx, u, i18n.DateShort, i18n.TimeShort)
		}
		return ui.Muted(html.Time(html.TimeConfig{Datetime: u.Format(time.RFC3339)}, render.Text(text)))
	}
	ends := ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween, NoWrap: true}, end(points[0].Time), end(now))
	// The card names its heading "<id>-title"; the chart is labelled by it.
	card := func(id string, title i18n.Phrase, bars []ui.BarChartBar) render.HTML {
		return ui.Card(ui.CardConfig{Heading: title.Text(ctx), Description: sp.bar.Text(ctx), ID: id},
			ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: bars, Width: 480, Height: 180, ShowAxis: true, HideValues: true, LabelledBy: id + "-title"}),
				ends))
	}
	return ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG},
		card("telemetry-chart-requests", i18nui.TelemetryChartRequests, requests),
		card("telemetry-chart-failed", i18nui.TelemetryChartFailed, failed),
		card("telemetry-chart-p95", i18nui.TelemetryChartP95, slowest))
}

func (s *Screens) routeTable(ctx context.Context, routes []itel.RouteStats, wkey string) render.HTML {
	left := max(len(routes)-routesShown, 0)
	routes = routes[:len(routes)-left]
	cols := []ui.Column{
		{Key: "route", Header: i18nui.TelemetryColRoute.Text(ctx), Wrap: true},
		{Key: "n", Header: i18nui.TelemetryColRequests.Text(ctx), Align: "end"},
		{Key: "failed", Header: i18nui.TelemetryColFailed.Text(ctx), Align: "end"},
		{Key: "slow", Header: i18nui.TelemetryColSlow.Text(ctx), Align: "end"},
		{Key: "p50", Header: i18nui.TelemetryColHalf.Text(ctx), Align: "end"},
		{Key: "p95", Header: i18nui.TelemetryColP95.Text(ctx), Align: "end"},
	}
	withShare := func(n, of int64) render.HTML {
		if n == 0 {
			return render.Text(i18n.Number(ctx, 0))
		}
		return render.Text(i18nui.TelemetryCountShare.Text(ctx, map[string]any{"count": i18n.Number(ctx, n), "share": share(ctx, n, of)}))
	}
	rows := make([]ui.Row, len(routes))
	for i, r := range routes {
		route := render.Text(r.Route)
		if r.Route != itel.OtherRoute {
			route = ui.Link(ui.LinkConfig{Href: link(s.RequestsPath(), "route", r.Route, "window", wkey), Text: r.Route})
		}
		rows[i] = ui.Row{ID: r.Route, Cells: map[string]render.HTML{
			"route":  route,
			"n":      render.Text(i18n.Number(ctx, r.N)),
			"failed": withShare(r.Failed, r.N),
			"slow":   withShare(r.Slow, r.N),
			"p50":    render.Text(within(ctx, r.P50)),
			"p95":    render.Text(within(ctx, r.P95)),
		}}
	}
	var note render.HTML
	if left > 0 {
		note = ui.Muted(render.Text(i18nui.TelemetryRoutesMore.Text(ctx, map[string]any{"n": i18n.Number(ctx, left)})))
	}
	// On a phone each route is a card that names every figure: a row of
	// bare numbers does not say which is the failures.
	return page(
		ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: rows, Ctx: ctx, Responsive: ui.ResponsiveCards,
			Caption: i18nui.TelemetryRoutes.Text(ctx)}),
		note)
}
