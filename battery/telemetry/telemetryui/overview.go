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

// routesShown is the rows of the overview's route table.
const routesShown = 50

// A chart is drawn at about the width of the card it sits in, on a
// phone and three across on a desktop alike. A drawing much wider than
// its card is scaled down to fit, and its axis text with it.
const chartWidth, chartHeight = 340, 160

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

// Overview draws the counts of every request in a time range: totals,
// how they moved over the range, and each route's share.
func (s *Screens) Overview(ctx context.Context) render.HTML { return s.overview(ctx, false) }

// OverviewRows draws the overview's figures alone: what the page
// re-fetches on its timer.
func (s *Screens) OverviewRows(ctx context.Context) render.HTML { return s.overview(ctx, true) }

func (s *Screens) overview(ctx context.Context, rowsOnly bool) render.HTML {
	q := query(ctx)
	sp := pickSpan(q)
	// A range whose bars are hours wide gains nothing from a refresh
	// every ten seconds, and it is the widest read: seven days of
	// counts, a third of a second of one core. It stays as loaded.
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
	wkey := sp.key
	if wkey == defaultWindow {
		wkey = ""
	}
	// The machine is read on its own clock, traffic or none. A window
	// it cannot be read for costs the page that section, not the rest.
	var machine render.HTML
	if host, err := store.Host(now.Add(-sp.span), now, sp.step); err != nil {
		slog.ErrorContext(ctx, "telemetry: read machine readings", "err", err)
	} else if !host.Latest.Time.IsZero() {
		machine = s.machine(ctx, sp, host, now)
	}
	// Queue jobs and scheduled runs, when the app has any: counted
	// apart, since nobody was waiting on them.
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
	// A change of range, and the timer's fetch, keep the tables' sorts.
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
	// "Slow" is a line the app's own settings draw: say where it is.
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

// byteText writes a size the way a machine's are read: whole megabytes
// under a gigabyte, gigabytes to one decimal over it.
func byteText(ctx context.Context, n int64) string {
	const mb, gb = 1 << 20, 1 << 30
	if n < gb {
		return i18nui.TelemetryBytesMB.Text(ctx, map[string]any{"n": i18n.Number(ctx, (n+mb/2)/mb)})
	}
	return i18nui.TelemetryBytesGB.Text(ctx, map[string]any{"n": i18n.Number(ctx, float64(n)/gb, i18n.MaxFraction(1))})
}

// diskWarnAt is how full a disk is drawn as trouble, whatever the
// app's own alert rule says: the default rule's line.
const diskWarnAt = 90

// machine draws the machine's latest reading and its CPU and memory
// over the range. A figure the platform does not give says so: only
// Linux reports the machine's CPU and memory.
func (s *Screens) machine(ctx context.Context, sp span, host itel.HostSeries, now time.Time) render.HTML {
	l := host.Latest
	// A figure the platform cannot give is a dash that says why, not
	// a sentence in the size of a number.
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
			// Said in words as well: red alone tells nothing to a
			// screen reader or a colour-blind reader.
			disk.Direction = ui.TrendDown
			disk.Trend = i18nui.TelemetryDiskNearlyFull.Text(ctx, map[string]any{"used": disk.Trend})
		}
	}
	// What this process holds: the kernel's count where there is one,
	// else the Go heap.
	held := l.RSSBytes
	if held == 0 {
		held = l.HeapBytes
	}
	proc := ui.StatCardConfig{Label: i18nui.TelemetryProcess.Text(ctx), Value: byteText(ctx, held), Plain: true,
		Trend: i18nui.TelemetryGoroutines.Text(ctx, map[string]any{"n": i18n.Number(ctx, l.Goroutines)})}
	strip := ui.StatStrip(ui.StatStripConfig{Label: i18nui.TelemetryMachineStats.Text(ctx), Cells: []render.HTML{
		ui.StatCard(cpu), ui.StatCard(mem), ui.StatCard(disk), ui.StatCard(proc)}})

	// A step with no reading draws no bar: the app was not running, or
	// the platform does not say.
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
		// Fill keeps these two the width of the three charts above
		// them, where two cards alone would stretch across the row.
		charts = ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG, Fill: true}, cards...)
	}
	read := i18nui.TelemetryUTC.Text(ctx, map[string]any{"time": i18n.Time(ctx, l.Time.UTC(), i18n.TimeShort)})
	if n := now.UTC(); l.Time.UTC().Year() != n.Year() || l.Time.UTC().YearDay() != n.YearDay() {
		read = i18nui.TelemetryUTC.Text(ctx, map[string]any{"time": i18n.DateTime(ctx, l.Time.UTC(), i18n.DateShort, i18n.TimeShort)})
	}
	return ui.Section(ui.SectionConfig{Heading: i18nui.TelemetryMachine.Text(ctx), Description: i18nui.TelemetryMachineSub.Text(ctx, map[string]any{"time": read}), Compact: true, Ctx: ctx},
		page(strip, charts))
}

// charts draws three questions over the range: how busy, when did it
// fail, when was it slow.
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
		// These sit straight under the page title.
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

// jobTable lists background work by name. It has no "slow": that line
// is drawn for someone waiting on a response.
func (s *Screens) jobTable(ctx context.Context, jobs []itel.RouteStats, q url.Values, wkey string) render.HTML {
	return s.countTable(ctx, jobs, q, wkey, i18nui.TelemetryJobs, i18nui.TelemetryColJob, i18nui.TelemetryColRuns, false, "jsort", "jdir")
}

// sortKeys are the columns a count table sorts by, and each one's
// order from most to least. The busiest come first unless asked: the
// question is as often "which fails most" or "which is slowest".
var sortKeys = map[string]func(a, b itel.RouteStats) int{
	"n":      func(a, b itel.RouteStats) int { return cmp.Compare(b.N, a.N) },
	"failed": func(a, b itel.RouteStats) int { return cmp.Compare(b.Failed, a.Failed) },
	"slow":   func(a, b itel.RouteStats) int { return cmp.Compare(b.Slow, a.Slow) },
	"p50":    func(a, b itel.RouteStats) int { return cmp.Compare(b.P50, a.P50) },
	"p95":    func(a, b itel.RouteStats) int { return cmp.Compare(b.P95, a.P95) },
	"route":  func(a, b itel.RouteStats) int { return strings.Compare(b.Route, a.Route) },
}

// overviewSorts are the query parameters the overview's two tables
// sort by: carried by every link and by the timer's fetch, so a sort
// outlives a change of range and a refresh.
var overviewSorts = []string{"sort", "dir", "jsort", "jdir"}

// sortOf reads one table's sort from the query: a column this table
// has, most first unless "asc". Anything else is the busiest first.
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

// countTable is the table of routes or of background work: each name's
// count, failures and how long it took, linked to its kept traces.
func (s *Screens) countTable(ctx context.Context, routes []itel.RouteStats, q url.Values, wkey string, caption, name, count i18n.Phrase, slow bool, sortParam, dirParam string) render.HTML {
	// Sorted before it is cut to length: the rows shown are the first
	// of the order asked for, not the busiest in that order.
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
	// A sort link keeps the range and the other table's sort.
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
		// A route holds spaces and braces, which an element id cannot.
		rows[i] = ui.Row{ID: sortParam + strconv.Itoa(i), Cells: cells}
	}
	var note render.HTML
	if left > 0 {
		note = ui.Muted(render.Text(i18nui.TelemetryRoutesMore.Text(ctx, map[string]any{"n": i18n.Number(ctx, left)})))
	}
	// On a phone each route is a card that names every figure: a row of
	// bare numbers does not say which is the failures.
	return page(
		ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: rows, Ctx: ctx, Responsive: ui.ResponsiveCards,
			Caption: caption.Text(ctx), SortBy: key, SortDir: dir,
			Path: s.OverviewPath(), Query: carried, SortParam: sortParam, DirParam: dirParam}),
		note)
}

// chartCard is one bar chart of a range in its card: the title, how
// wide a bar is, the bars, and under them where they start and end to
// the minute. The first bar's start carries its date when that is not
// today. The card names its heading "<id>-title"; the chart is labelled
// by it. top, when not zero, is the top of the scale: 100 for a
// percentage.
// barTime names a bar's start: the time alone for today's, the date
// too for an earlier day's.
func barTime(ctx context.Context, t, now time.Time) string {
	u, n := t.UTC(), now.UTC()
	if u.Year() == n.Year() && u.YearDay() == n.YearDay() {
		return i18n.Time(ctx, u, i18n.TimeShort)
	}
	return i18n.DateTime(ctx, u, i18n.DateShort, i18n.TimeShort)
}

// percentOf100 writes a chart value that is already a percentage.
func percentOf100(ctx context.Context, v float64) string { return i18n.Percent(ctx, v/100) }

// peak says a chart's highest bar in words, or nothing when every bar
// is empty. A bar chart is a picture: this sentence is what it shows to
// someone who cannot see it, and the first thing anyone looks for in
// it. Of equal bars the latest wins, the one nearest to now.
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
	// Every other time on these screens says its zone; so do these.
	end := func(t time.Time, zone bool) render.HTML {
		u := t.UTC()
		text := i18n.Time(ctx, u, i18n.TimeShort)
		// A chart that starts on another day dates both ends: a bare
		// time at the right of a dated one reads as the same day.
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
