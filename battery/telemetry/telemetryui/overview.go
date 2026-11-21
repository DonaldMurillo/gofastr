package telemetryui

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

const routesShown = 50

const chartWidth, chartHeight = 340, 160

const liveStepMax = 15 * time.Minute

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

func pickSpan(q url.Values) span {
	key := choice(q, "window")
	for _, sp := range spans {
		if sp.key == key {
			return sp
		}
	}
	return spans[1]
}

// OverviewPath is where the host serves the overview page.
func (s *Screens) OverviewPath() string { return s.base + "/overview" }

// Overview draws the counts of every request in a time range: totals, how
// they moved over the range, and each route's share.
func (s *Screens) Overview(ctx context.Context) render.HTML { return s.overview(ctx, false) }

// OverviewRows draws the overview's figures alone: what the page
// re-fetches on its timer.
func (s *Screens) OverviewRows(ctx context.Context) render.HTML { return s.overview(ctx, true) }

func (s *Screens) overview(ctx context.Context, rowsOnly bool) render.HTML {
	q := query(ctx)
	sp := pickSpan(q)
	live := s.Live != nil && sp.step <= liveStepMax
	header := ui.PageHeaderConfig{
		Title: i18nui.TelemetryOverview.Text(ctx),
	}
	if live {
		header.Actions = s.liveBadge(ctx, true)
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
	wkey := windowParam(sp.key)
	var machine render.HTML
	if host, err := store.Host(now.Add(-sp.span), now, sp.step); err != nil {
		slog.ErrorContext(ctx, "telemetry: read machine readings", "err", err)
	} else if !host.Latest.Time.IsZero() {
		machine = s.machine(ctx, sp, host, now)
	}
	var jobs render.HTML
	if len(stats.Jobs) > 0 {
		jobs = s.jobTable(ctx, stats.Jobs, q, wkey)
	}
	var body render.HTML
	if stats.Total.N == 0 {
		body = page(ui.EmptyState(ui.EmptyStateConfig{
			Title:       i18nui.TelemetryOverviewEmpty.Text(ctx),
			Description: i18nui.TelemetryOverviewEmptySub.Text(ctx),
		}), jobs, machine)
	} else {
		body = page(s.totals(ctx, stats.Total, wkey), s.charts(ctx, sp, stats.Points, now), s.routeTable(ctx, stats.Routes, q, wkey), jobs, machine)
	}
	if rowsOnly {
		return body
	}
	choices := make([]window, len(spans))
	for i, o := range spans {
		choices[i] = o.window
	}
	sorted := func(path, window string) string {
		pairs := []string{"window", window}
		for _, name := range overviewSorts {
			pairs = append(pairs, name, param(q, name))
		}
		return link(path, pairs...)
	}
	filters := rangeLinks(ctx, choices, sp.key, func(key string) string { return sorted(s.OverviewPath(), key) })
	if live {
		body = s.rows(true, sorted(s.Live("overview"), wkey), body)
	}
	return page(s.tabNav(ctx, tabOverview), ui.PageHeader(header), s.firing(ctx), filters, body)
}

func share(ctx context.Context, part, whole int64) string {
	if whole <= 0 {
		return i18n.Percent(ctx, 0)
	}
	f := float64(part) / float64(whole)
	switch {
	case part > 0 && f < 0.001:
		return i18n.Percent(ctx, max(f, 0.0001), i18n.MaxFraction(2))
	case part > 0 && f < 0.1:
		return i18n.Percent(ctx, f, i18n.MaxFraction(1))
	}
	return i18n.Percent(ctx, f)
}

func within(ctx context.Context, d time.Duration) string {
	text := ui.DurationText(ctx, itel.RoundDuration(d))
	if d >= itel.StatsCeiling {
		return i18nui.TelemetryOrMore.Text(ctx, map[string]any{"duration": text})
	}
	return text
}

func (s *Screens) totals(ctx context.Context, t itel.StatsCount, wkey string) render.HTML {
	ofAll := func(n int64) string {
		return i18nui.TelemetryStatShare.Text(ctx, map[string]any{"share": share(ctx, n, t.N)})
	}
	slowLabel := i18nui.TelemetryStatSlow.Text(ctx)
	if at := s.tel.SlowThreshold(); at > 0 {
		slowLabel = i18nui.TelemetryStatSlowAt.Text(ctx, map[string]any{"duration": ui.DurationText(ctx, at)})
	}
	failed := ui.StatCardConfig{Label: i18nui.TelemetryStatFailed.Text(ctx), Value: i18n.Number(ctx, t.Failed), Trend: ofAll(t.Failed), Plain: true,
		Href: link(s.RequestsPath(), "show", "failed", "window", wkey)}
	if t.Failed > 0 {
		failed.Direction = ui.TrendDown
	}
	return ui.StatStrip(ui.StatStripConfig{Label: i18nui.TelemetryStats.Text(ctx), Cells: []render.HTML{
		ui.StatCard(ui.StatCardConfig{Label: i18nui.TelemetryChartRequests.Text(ctx), Value: i18n.Number(ctx, t.N), Plain: true}),
		ui.StatCard(failed),
		ui.StatCard(ui.StatCardConfig{Label: slowLabel, Value: i18n.Number(ctx, t.Slow), Trend: ofAll(t.Slow), Plain: true,
			Href: link(s.RequestsPath(), "show", "slow", "window", wkey)}),
		ui.StatCard(ui.StatCardConfig{Label: i18nui.TelemetryStatP95.Text(ctx), Value: within(ctx, t.P95), Plain: true}),
	}})
}

// byteText writes a size the way a machine's are read.
func byteText(ctx context.Context, n int64) string {
	v, unit := itel.ByteSize(n)
	if unit == "GB" {
		return i18nui.TelemetryBytesGB.Text(ctx, map[string]any{"n": i18n.Number(ctx, v, i18n.MaxFraction(1))})
	}
	return i18nui.TelemetryBytesMB.Text(ctx, map[string]any{"n": i18n.Number(ctx, v)})
}

const diskWarnAt = 90

func (s *Screens) machine(ctx context.Context, sp span, host itel.HostSeries, now time.Time) render.HTML {
	l := host.Latest
	const dash = "–"
	unknown := i18nui.TelemetryNotKnown.Text(ctx)
	usedOf := func(used, total int64) string {
		return i18nui.TelemetryUsedOf.Text(ctx, map[string]any{"used": byteText(ctx, used), "total": byteText(ctx, total)})
	}
	cpu := ui.StatCardConfig{Label: i18nui.TelemetryCPU.Text(ctx), Value: dash, Trend: unknown, Plain: true}
	if l.CPU >= 0 {
		cpu.Value, cpu.Trend = i18n.Percent(ctx, l.CPU), i18nui.TelemetryAllCores.Text(ctx)
	}
	mem := ui.StatCardConfig{Label: i18nui.TelemetryMemory.Text(ctx), Value: dash, Trend: unknown, Plain: true}
	if p := l.MemPercent(); p >= 0 {
		mem.Value, mem.Trend = i18n.Percent(ctx, float64(p)/100), usedOf(l.MemUsed, l.MemTotal)
	}
	disk := ui.StatCardConfig{Label: i18nui.TelemetryDisk.Text(ctx), Value: dash, Trend: unknown, Plain: true}
	if p := l.DiskPercent(); p >= 0 {
		disk.Value, disk.Trend = i18n.Percent(ctx, float64(p)/100), usedOf(l.DiskUsed, l.DiskTotal)
		if p >= diskWarnAt {
			disk.Direction = ui.TrendDown
			disk.Trend = i18nui.TelemetryDiskNearlyFull.Text(ctx, map[string]any{"used": disk.Trend})
		}
	}
	proc := ui.StatCardConfig{Label: i18nui.TelemetryProcess.Text(ctx), Value: byteText(ctx, l.HeldBytes()), Plain: true,
		Trend: i18nui.TelemetryGoroutines.Text(ctx, map[string]any{"n": i18n.Number(ctx, l.Goroutines)})}
	strip := ui.StatStrip(ui.StatStripConfig{Label: i18nui.TelemetryMachineStats.Text(ctx), Cells: []render.HTML{
		ui.StatCard(cpu), ui.StatCard(mem), ui.StatCard(disk), ui.StatCard(proc)}})

	var haveCPU, haveMem bool
	cpuBars := make([]ui.BarChartBar, len(host.Points))
	memBars := make([]ui.BarChartBar, len(host.Points))
	for i, p := range host.Points {
		at := barTime(ctx, p.Time, now)
		cpuBars[i] = ui.BarChartBar{Label: at, Value: math.NaN()}
		memBars[i] = ui.BarChartBar{Label: at, Value: math.NaN(), Color: "info"}
		if p.CPU >= 0 {
			cpuBars[i].Value, haveCPU = math.Round(p.CPU*1000)/10, true
		}
		if p.MemTotal > 0 {
			memBars[i].Value, haveMem = math.Round(float64(min(p.MemUsed, p.MemTotal))/float64(p.MemTotal)*1000)/10, true
		}
	}
	var cards []render.HTML
	if haveCPU {
		cards = append(cards, s.chartCard(ctx, sp, "telemetry-chart-cpu", i18nui.TelemetryChartCPU, cpuBars, 100, host.Points[0].Time, now, percentOf100, 3))
	}
	if haveMem {
		cards = append(cards, s.chartCard(ctx, sp, "telemetry-chart-memory", i18nui.TelemetryChartMemory, memBars, 100, host.Points[0].Time, now, percentOf100, 3))
	}
	var charts render.HTML
	if len(cards) > 0 {
		charts = ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG, Fill: true}, cards...)
	}
	read := i18nui.TelemetryUTC.Text(ctx, map[string]any{"time": i18n.Time(ctx, l.Time.UTC(), i18n.TimeShort)})
	if n := now.UTC(); l.Time.UTC().Year() != n.Year() || l.Time.UTC().YearDay() != n.YearDay() {
		read = i18nui.TelemetryUTC.Text(ctx, map[string]any{"time": i18n.DateTime(ctx, l.Time.UTC(), i18n.DateShort, i18n.TimeShort)})
	}
	return ui.Section(ui.SectionConfig{Heading: i18nui.TelemetryMachine.Text(ctx), Description: i18nui.TelemetryMachineSub.Text(ctx, map[string]any{"time": read}), Compact: true, Ctx: ctx},
		page(strip, charts))
}

func (s *Screens) charts(ctx context.Context, sp span, points []itel.StatsPoint, now time.Time) render.HTML {
	requests := make([]ui.BarChartBar, len(points))
	failed := make([]ui.BarChartBar, len(points))
	slowest := make([]ui.BarChartBar, len(points))
	for i, p := range points {
		at := barTime(ctx, p.Time, now)
		requests[i] = ui.BarChartBar{Label: at, Value: float64(p.N)}
		failed[i] = ui.BarChartBar{Label: at, Value: float64(p.Failed), Color: "danger"}
		slowest[i] = ui.BarChartBar{Label: at, Value: float64(p.P95.Microseconds()) / 1000, Color: "warning"}
	}
	first := points[0].Time
	card := func(id string, title i18n.Phrase, bars []ui.BarChartBar, figure func(context.Context, float64) string) render.HTML {
		return s.chartCard(ctx, sp, id, title, bars, 0, first, now, figure, 2)
	}
	whole := func(ctx context.Context, v float64) string { return i18n.Number(ctx, int(v)) }
	millis := func(ctx context.Context, v float64) string {
		return ui.DurationText(ctx, time.Duration(v*float64(time.Millisecond)))
	}
	return ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG},
		card("telemetry-chart-requests", i18nui.TelemetryChartRequests, requests, whole),
		card("telemetry-chart-failed", i18nui.TelemetryChartFailed, failed, whole),
		card("telemetry-chart-p95", i18nui.TelemetryChartP95, slowest, millis))
}

func (s *Screens) routeTable(ctx context.Context, routes []itel.RouteStats, q url.Values, wkey string) render.HTML {
	return s.countTable(ctx, routes, q, wkey, i18nui.TelemetryRoutes, i18nui.TelemetryColRoute, i18nui.TelemetryColRequests, true, "sort", "dir")
}

func (s *Screens) jobTable(ctx context.Context, jobs []itel.RouteStats, q url.Values, wkey string) render.HTML {
	return s.countTable(ctx, jobs, q, wkey, i18nui.TelemetryJobs, i18nui.TelemetryColJob, i18nui.TelemetryColRuns, false, "jsort", "jdir")
}

var sortKeys = map[string]func(a, b itel.RouteStats) int{
	"n":      func(a, b itel.RouteStats) int { return cmp.Compare(b.N, a.N) },
	"failed": func(a, b itel.RouteStats) int { return cmp.Compare(b.Failed, a.Failed) },
	"slow":   func(a, b itel.RouteStats) int { return cmp.Compare(b.Slow, a.Slow) },
	"p50":    func(a, b itel.RouteStats) int { return cmp.Compare(b.P50, a.P50) },
	"p95":    func(a, b itel.RouteStats) int { return cmp.Compare(b.P95, a.P95) },
	"route":  func(a, b itel.RouteStats) int { return strings.Compare(b.Route, a.Route) },
}

var overviewSorts = []string{"sort", "dir", "jsort", "jdir"}

func sortOf(q url.Values, sortParam, dirParam string, slow bool) (key string, dir ui.SortDir) {
	key, dir = param(q, sortParam), ui.SortDesc
	if _, ok := sortKeys[key]; !ok || (key == "slow" && !slow) {
		return "n", ui.SortDesc
	}
	if param(q, dirParam) == "asc" {
		dir = ui.SortAsc
	}
	return key, dir
}

func (s *Screens) countTable(ctx context.Context, routes []itel.RouteStats, q url.Values, wkey string, caption, name, count i18n.Phrase, slow bool, sortParam, dirParam string) render.HTML {
	key, dir := sortOf(q, sortParam, dirParam, slow)
	routes = slices.Clone(routes)
	slices.SortStableFunc(routes, func(a, b itel.RouteStats) int {
		c := sortKeys[key](a, b)
		if dir == ui.SortAsc {
			c = -c
		}
		return c
	})
	left := max(len(routes)-routesShown, 0)
	routes = routes[:len(routes)-left]
	cols := []ui.Column{
		{Key: "route", Header: name.Text(ctx), Wrap: true, Sortable: true},
		{Key: "n", Header: count.Text(ctx), Align: "end", Sortable: true},
		{Key: "failed", Header: i18nui.TelemetryColFailed.Text(ctx), Align: "end", Sortable: true},
	}
	if slow {
		cols = append(cols, ui.Column{Key: "slow", Header: i18nui.TelemetryColSlow.Text(ctx), Align: "end", Sortable: true})
	}
	cols = append(cols,
		ui.Column{Key: "p50", Header: i18nui.TelemetryColHalf.Text(ctx), Align: "end", Sortable: true},
		ui.Column{Key: "p95", Header: i18nui.TelemetryColP95.Text(ctx), Align: "end", Sortable: true})
	carried := url.Values{}
	if wkey != "" {
		carried.Set("window", wkey)
	}
	for _, name := range overviewSorts {
		if v := param(q, name); v != "" {
			carried.Set(name, v)
		}
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
		cells := map[string]render.HTML{
			"route":  route,
			"n":      render.Text(i18n.Number(ctx, r.N)),
			"failed": withShare(r.Failed, r.N),
			"p50":    render.Text(within(ctx, r.P50)),
			"p95":    render.Text(within(ctx, r.P95)),
		}
		if slow {
			cells["slow"] = withShare(r.Slow, r.N)
		}
		rows[i] = ui.Row{ID: sortParam + strconv.Itoa(i), Cells: cells}
	}
	var note render.HTML
	if left > 0 {
		note = ui.Muted(render.Text(i18nui.TelemetryRoutesMore.Text(ctx, map[string]any{"n": i18n.Number(ctx, left)})))
	}
	return page(
		ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: rows, Ctx: ctx, Responsive: ui.ResponsiveCards,
			Caption: caption.Text(ctx), SortBy: key, SortDir: dir,
			Path: s.OverviewPath(), Query: carried, SortParam: sortParam, DirParam: dirParam}),
		note)
}

func barTime(ctx context.Context, t, now time.Time) string {
	u, n := t.UTC(), now.UTC()
	if u.Year() == n.Year() && u.YearDay() == n.YearDay() {
		return i18n.Time(ctx, u, i18n.TimeShort)
	}
	return i18n.DateTime(ctx, u, i18n.DateShort, i18n.TimeShort)
}

func percentOf100(ctx context.Context, v float64) string { return i18n.Percent(ctx, v/100) }

func peak(ctx context.Context, bars []ui.BarChartBar, figure func(context.Context, float64) string) string {
	at := -1
	for i, b := range bars {
		if b.Value > 0 && (at < 0 || b.Value >= bars[at].Value) {
			at = i
		}
	}
	if at < 0 {
		return ""
	}
	return i18nui.TelemetryChartPeak.Text(ctx, map[string]any{"value": figure(ctx, bars[at].Value), "time": bars[at].Label})
}

func (s *Screens) chartCard(ctx context.Context, sp span, id string, title i18n.Phrase, bars []ui.BarChartBar, top float64, first, now time.Time, figure func(context.Context, float64) string, level int) render.HTML {
	about := sp.bar.Text(ctx)
	if p := peak(ctx, bars, figure); p != "" {
		about += " " + p
	}
	end := func(t time.Time, zone bool) render.HTML {
		u := t.UTC()
		text := i18n.Time(ctx, u, i18n.TimeShort)
		if f, n := first.UTC(), now.UTC(); f.Year() != n.Year() || f.YearDay() != n.YearDay() {
			text = i18n.DateTime(ctx, u, i18n.DateShort, i18n.TimeShort)
		}
		if zone {
			text = i18nui.TelemetryUTC.Text(ctx, map[string]any{"time": text})
		}
		return ui.Muted(html.Time(html.TimeConfig{Datetime: u.Format(time.RFC3339)}, render.Text(text)))
	}
	return ui.Card(ui.CardConfig{Heading: title.Text(ctx), Description: about, ID: id, HeadingLevel: level},
		ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			ui.BarChart(ui.BarChartConfig{Ctx: ctx, Bars: bars, Width: chartWidth, Height: chartHeight, ShowAxis: true, HideValues: true, Max: top, LabelledBy: id + "-title"}),
			ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween, NoWrap: true}, end(first, false), end(now, true))))
}
