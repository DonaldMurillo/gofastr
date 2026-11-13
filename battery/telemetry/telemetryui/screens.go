// Package telemetryui draws the request and log screens over the files
// battery/telemetry writes: the kept requests, one request's steps and
// log lines, and the log itself, each filtered through its query
// string. New mounts them on a path of their own, behind a gate the app
// supplies; battery/telemetry/telemetryadmin puts the same screens in
// the admin instead.
//
// Experimental, with battery/telemetry.
package telemetryui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// Page sizes, and the longest filter value read from a query string.
const (
	requestsPerPage = 50
	logsPerPage     = 100
	maxParam        = 256
)

// Screens draws the pages for one telemetry plugin at one place in the
// app's URLs.
type Screens struct {
	tel  *telemetry.Plugin
	base string
	tabs bool
	now  func() time.Time
}

// NewScreens returns the screens for tel, linking to each other under
// base ("/telemetry", "/admin/telemetry"). With tabs, each page draws
// the links between the two itself, for a host with no navigation of
// its own.
func NewScreens(tel *telemetry.Plugin, base string, tabs bool) *Screens {
	return &Screens{tel: tel, base: strings.TrimRight(base, "/"), tabs: tabs, now: time.Now}
}

// RequestsPath and LogsPath are where the host serves the two pages.
func (s *Screens) RequestsPath() string { return s.base + "/requests" }

// LogsPath is where the host serves the logs page.
func (s *Screens) LogsPath() string { return s.base + "/logs" }

// param reads one query value, cut to what a filter can need.
func param(q url.Values, name string) string {
	v := q.Get(name)
	if len(v) > maxParam {
		v = v[:maxParam]
	}
	return v
}

func query(ctx context.Context) url.Values {
	if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
		return r.URL.Query()
	}
	return url.Values{}
}

// window is one choice of time range.
type window struct {
	key   string
	label i18n.Phrase
	span  time.Duration // 0: everything kept
}

var windows = []window{
	{"1h", i18nui.TelemetryWindowHour, time.Hour},
	{"24h", i18nui.TelemetryWindowDay, 24 * time.Hour},
	{"7d", i18nui.TelemetryWindowWeek, 7 * 24 * time.Hour},
	{"all", i18nui.TelemetryWindowAll, 0},
}

const defaultWindow = "24h"

// pickWindow returns the chosen range and where it starts.
func (s *Screens) pickWindow(q url.Values) (window, time.Time) {
	key := param(q, "window")
	for _, w := range windows {
		if w.key == key || (key == "" && w.key == defaultWindow) {
			if w.span == 0 {
				return w, time.Time{}
			}
			return w, s.now().Add(-w.span)
		}
	}
	return s.pickWindow(url.Values{})
}

// before reads the paging cursor: list lines older than this.
func before(q url.Values) time.Time {
	t, err := time.Parse(time.RFC3339Nano, param(q, "before"))
	if err != nil {
		return time.Time{}
	}
	return t
}

// link builds a URL for path carrying the non-empty pairs.
func link(path string, pairs ...string) string {
	q := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			q.Set(pairs[i], pairs[i+1])
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func (s *Screens) tabNav(ctx context.Context, logs bool) render.HTML {
	if !s.tabs {
		return ""
	}
	return ui.TabNav(ui.TabNavConfig{
		Label: i18nui.TelemetryNav.Text(ctx),
		Items: []ui.TabNavItem{
			{Text: i18nui.TelemetryRequests.Text(ctx), Href: s.RequestsPath(), Current: !logs},
			{Text: i18nui.TelemetryLogs.Text(ctx), Href: s.LogsPath(), Current: logs},
		},
	})
}

func page(parts ...render.HTML) render.HTML {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, kept...)
}

func problem(ctx context.Context, msg i18n.Phrase) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(msg.Text(ctx)))
}

// timeCell shows when something happened to the second, which is what
// lines up a log with a report of trouble; how long ago is the tooltip.
func timeCell(ctx context.Context, now, t time.Time) render.HTML {
	u := t.UTC()
	return html.Time(html.TimeConfig{Datetime: u.Format(time.RFC3339Nano),
		ExtraAttrs: html.Attrs{"title": ui.Ago(ctx, now, t)}},
		render.Text(i18n.DateTime(ctx, u, i18n.DateMedium, i18n.TimeLong)))
}

func (s *Screens) windowFacet(ctx context.Context, current window) ui.Facet {
	f := ui.Facet{Name: "window", Label: i18nui.TelemetryWindow.Text(ctx), Value: current.key}
	for _, w := range windows {
		f.Options = append(f.Options, ui.FacetOption{Label: w.label.Text(ctx), Value: w.key})
	}
	return f
}

// older is the link to the next page, drawn when this one is full.
func older(ctx context.Context, full bool, href, newest string, paged bool) render.HTML {
	var links []render.HTML
	if paged {
		links = append(links, ui.LinkButton(ui.LinkButtonConfig{Label: i18nui.TelemetryNewest.Text(ctx), Href: newest, Variant: ui.ButtonSecondary}))
	}
	if full {
		links = append(links, ui.LinkButton(ui.LinkButtonConfig{Label: i18nui.TelemetryOlder.Text(ctx), Href: href, Variant: ui.ButtonSecondary}))
	}
	if len(links) == 0 {
		return ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Justify: ui.JustifyEnd}, links...)
}

// ----- requests ----------------------------------------------------------------

// Requests draws the list of kept requests, or one request when the
// query names a trace.
func (s *Screens) Requests(ctx context.Context) render.HTML {
	q := query(ctx)
	if id := strings.ToLower(strings.TrimSpace(param(q, "trace"))); id != "" {
		return s.request(ctx, id)
	}
	store := s.tel.Reader()
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryRequests.Text(ctx),
		Subtitle: i18nui.TelemetryRequestsSub.Text(ctx),
	})
	if store == nil {
		return page(s.tabNav(ctx, false), header, problem(ctx, i18nui.TelemetryNotReady))
	}

	show, route := param(q, "show"), param(q, "route")
	win, from := s.pickWindow(q)
	cursor := before(q)
	tq := itel.TraceQuery{Route: route, From: from, To: cursor}
	switch show {
	case "failed":
		tq.Failed = true
	case "slow":
		tq.MinDur = s.tel.SlowThreshold()
	default:
		show = ""
	}
	traces, err := store.Traces(tq, requestsPerPage)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: list requests", "err", err)
		return page(s.tabNav(ctx, false), header, problem(ctx, i18nui.TelemetryLoadFailed))
	}

	path := s.RequestsPath()
	here := func(show, route, before string) string {
		w := win.key
		if w == defaultWindow {
			w = ""
		}
		return link(path, "show", show, "route", route, "window", w, "before", before)
	}
	segments := ui.SegmentedLinks(ui.SegmentedLinksConfig{
		Label: i18nui.TelemetryShow.Text(ctx), Ctx: ctx,
		Items: []ui.SegmentLink{
			{Text: i18nui.TelemetryShowAll.Text(ctx), Href: here("", route, ""), Current: show == ""},
			{Text: i18nui.TelemetryShowFailed.Text(ctx), Href: here("failed", route, ""), Current: show == "failed"},
			{Text: i18nui.TelemetryShowSlow.Text(ctx), Href: here("slow", route, ""), Current: show == "slow"},
		},
	})
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: path, ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Search:     &ui.FilterSearch{Name: "trace", Label: i18nui.TelemetryFindTrace.Text(ctx), Placeholder: i18nui.TelemetryTraceIDHint.Text(ctx)},
		Facets:     []ui.Facet{s.windowFacet(ctx, win)},
		Hidden:     []ui.HiddenField{{Name: "show", Value: show}, {Name: "route", Value: route}},
		HideReset:  true,
	})
	var routeChip render.HTML
	if route != "" {
		routeChip = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
			ui.Tag(ui.TagConfig{Ctx: ctx, Label: i18nui.TelemetryRouteFilter.Text(ctx, map[string]any{"route": route})}),
			ui.Link(ui.LinkConfig{Href: here(show, "", ""), Text: i18nui.TelemetryRouteClear.Text(ctx), Variant: ui.LinkMuted}))
	}

	now := s.now()
	cols := []ui.Column{
		{Key: "route", Header: i18nui.TelemetryColRoute.Text(ctx), Phone: ui.PhoneTitle, Wrap: true},
		{Key: "status", Header: i18nui.TelemetryColStatus.Text(ctx), Phone: ui.PhoneMeta},
		{Key: "took", Header: i18nui.TelemetryColDuration.Text(ctx), Align: "end", Phone: ui.PhoneDetail},
		{Key: "time", Header: i18nui.TelemetryColTime.Text(ctx), Phone: ui.PhoneSubtitle},
		{Key: "open", Header: i18nui.TelemetryColTrace.Text(ctx), Align: "end", Fit: true, Phone: ui.PhoneEnd},
	}
	rows := make([]ui.Row, len(traces))
	for i, t := range traces {
		routeCell := render.HTML(render.Text(t.Route))
		if route == "" && t.Route != "" {
			routeCell = ui.Link(ui.LinkConfig{Href: here(show, t.Route, ""), Text: t.Route,
				ExtraAttrs: html.Attrs{"title": i18nui.TelemetryRouteOnly.Text(ctx, map[string]any{"route": t.Route})}})
		}
		// The path the caller asked for, when the route is a pattern
		// (or "unmatched") and so does not say.
		if p := pathBeside(t.Route, t.Path); p != "" {
			routeCell = ui.Stack(ui.StackConfig{Gap: ui.GapNone}, routeCell, ui.Muted(render.Text(p)))
		}
		took := render.HTML(render.Text(ui.DurationText(ctx, t.Dur)))
		if slow := s.tel.SlowThreshold(); slow > 0 && t.Dur >= slow {
			took = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter, Justify: ui.JustifyEnd, NoWrap: true},
				ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.TelemetrySlow.Text(ctx), Variant: ui.StatusWarning}), took)
		}
		rows[i] = ui.Row{ID: t.TraceID, Cells: map[string]render.HTML{
			"route":  routeCell,
			"status": statusBadge(ctx, t.Status, t.Error, t.FailedSteps),
			"took":   took,
			"time":   timeCell(ctx, now, t.Time),
			"open": ui.Link(ui.LinkConfig{Href: link(path, "trace", t.TraceID), Text: i18nui.TelemetryOpenTrace.Text(ctx), Variant: ui.LinkAction,
				ExtraAttrs: html.Attrs{"aria-label": i18nui.TelemetryOpenTraceOf.Text(ctx, map[string]any{
					"route": t.Route, "time": i18n.DateTime(ctx, t.Time.UTC(), i18n.DateMedium, i18n.TimeLong)})}}),
		}}
	}
	table := ui.DataTable(ui.DataTableConfig{
		Columns: cols, Rows: rows, Ctx: ctx,
		Caption: i18nui.TelemetryRequests.Text(ctx), CaptionHidden: true,
		Responsive: ui.ResponsiveRows,
		Empty: ui.EmptyStateConfig{
			Title:       i18nui.TelemetryRequestsEmpty.Text(ctx),
			Description: i18nui.TelemetryRequestsEmptySub.Text(ctx),
		},
	})
	var next string
	if len(traces) == requestsPerPage {
		next = here(show, route, traces[len(traces)-1].Time.Add(-time.Nanosecond).UTC().Format(time.RFC3339Nano))
	}
	return page(s.tabNav(ctx, false), header, segments, filters, routeChip, table,
		older(ctx, next != "", next, here(show, route, ""), !cursor.IsZero()))
}

// pathBeside returns path when it says something route does not: the
// route is a pattern ("GET /orders/{id}") or "unmatched". A route that
// already ends in the path gets nothing beside it.
func pathBeside(route, path string) string {
	if path == "" || route == path || strings.HasSuffix(route, " "+path) {
		return ""
	}
	return path
}

// statusBadge is a request's outcome: its HTTP status, colored by what
// the status means, and beside it a note when a step inside failed
// although the request as a whole did not. Work with no status (a job)
// is its failure or nothing.
func statusBadge(ctx context.Context, status int, failed bool, failedSteps int) render.HTML {
	label, variant := strconv.Itoa(status), ui.StatusSuccess
	switch {
	case status == 0 && failed:
		label, variant = i18nui.TelemetryFailed.Text(ctx), ui.StatusDanger
	case status == 0:
		label, variant = i18nui.TelemetryNoStatus.Text(ctx), ui.StatusNeutral
	case status >= 500 || failed:
		variant = ui.StatusDanger
	case status >= 400:
		variant = ui.StatusWarning
	}
	badge := ui.StatusBadge(ui.StatusBadgeConfig{Label: label, Variant: variant})
	if failedSteps > 0 && !failed {
		return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
			badge, ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.TelemetryStepFailed.Text(ctx), Variant: ui.StatusDanger}))
	}
	return badge
}

// request draws one request: its summary, its steps on a time axis, and
// the log lines that carry its trace ID.
func (s *Screens) request(ctx context.Context, id string) render.HTML {
	back := ui.LinkButton(ui.LinkButtonConfig{Label: i18nui.TelemetryBack.Text(ctx), Href: s.RequestsPath(), Variant: ui.ButtonSecondary})
	missing := func(title string, body i18n.Phrase) render.HTML {
		return page(s.tabNav(ctx, false),
			ui.PageHeader(ui.PageHeaderConfig{Eyebrow: i18nui.TelemetryRequests.Text(ctx), Title: title, Actions: back}),
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusWarning}, render.Text(body.Text(ctx))))
	}
	store := s.tel.Reader()
	if store == nil {
		return missing(i18nui.TelemetryRequest.Text(ctx), i18nui.TelemetryNotReady)
	}
	line, ok, err := store.TraceByID(id)
	switch {
	case errors.Is(err, itel.ErrBadTraceID):
		return missing(i18nui.TelemetryTraceMissing.Text(ctx), i18nui.TelemetryTraceBadID)
	case err != nil:
		slog.ErrorContext(ctx, "telemetry: read request", "err", err)
		return missing(i18nui.TelemetryRequest.Text(ctx), i18nui.TelemetryLoadFailed)
	case !ok:
		return missing(i18nui.TelemetryTraceMissing.Text(ctx), i18nui.TelemetryTraceMissingSub)
	}
	tr, err := itel.ParseTrace(line)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: parse request", "err", err)
		return missing(i18nui.TelemetryRequest.Text(ctx), i18nui.TelemetryLoadFailed)
	}

	title := tr.Route
	if title == "" {
		title = i18nui.TelemetryRequest.Text(ctx)
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow: i18nui.TelemetryRequests.Text(ctx), Title: title, Subtitle: pathBeside(tr.Route, tr.Path),
		Badge: statusBadge(ctx, tr.Status, tr.Error, tr.FailedSteps), Actions: back,
	})
	summary := ui.MetricBand(ui.MetricBandConfig{
		Label: i18nui.TelemetrySummary.Text(ctx),
		Items: []ui.MetricBandItem{
			{Label: i18nui.TelemetryColDuration.Text(ctx), Value: ui.DurationText(ctx, tr.Dur)},
			{Label: i18nui.TelemetryEnded.Text(ctx), Value: i18n.DateTime(ctx, tr.Time.UTC(), i18n.DateMedium, i18n.TimeLong)},
			{Label: i18nui.TelemetryStepsCount.Text(ctx), Value: i18n.Number(ctx, len(tr.Spans))},
		},
	})
	steps := ui.Section(ui.SectionConfig{Heading: i18nui.TelemetrySteps.Text(ctx), Compact: true, Ctx: ctx},
		ui.Waterfall(ui.WaterfallConfig{
			Label: i18nui.TelemetryStepsOf.Text(ctx, map[string]any{"route": title}),
			Total: tr.Dur, Steps: waterfallSteps(tr), Ctx: ctx,
		}))

	start := tr.Time.Add(-tr.Dur)
	lines, err := store.LogsForTrace(tr.TraceID, start.Add(-time.Second), tr.Time.Add(time.Second))
	var logs render.HTML
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: read request logs", "err", err)
		logs = problem(ctx, i18nui.TelemetryLoadFailed)
	} else {
		logs = s.logTable(ctx, lines, false, i18nui.TelemetryRequestLogsEmpty, i18n.Phrase{})
	}
	return page(s.tabNav(ctx, false), header, summary,
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: i18nui.TelemetryTraceID.Text(ctx), Value: ui.InlineCode(tr.TraceID)},
		}}),
		steps,
		ui.Section(ui.SectionConfig{Heading: i18nui.TelemetryRequestLogs.Text(ctx), Description: i18nui.TelemetryRequestLogsSub.Text(ctx), Compact: true, Ctx: ctx}, logs))
}

// detailAttrs are the span attributes worth a line under a step's name,
// first match wins: what was run, or what was called.
var detailAttrs = []string{"db.query.text", "db.statement", "http.url", "url.full", "http.target", "messaging.destination.name"}

// waterfallSteps orders a trace's spans as a tree, each under its
// parent and siblings by start, and gives every one its depth. A span
// whose parent is not in the trace hangs from the top.
func waterfallSteps(tr itel.Trace) []ui.WaterfallStep {
	children := map[string][]int{}
	known := map[string]bool{}
	for _, sp := range tr.Spans {
		known[sp.SpanID] = true
	}
	var roots []int
	for i, sp := range tr.Spans {
		if sp.ParentID != "" && known[sp.ParentID] && sp.ParentID != sp.SpanID {
			children[sp.ParentID] = append(children[sp.ParentID], i)
		} else {
			roots = append(roots, i)
		}
	}
	byStart := func(a, b int) int { return int(tr.Spans[a].Offset - tr.Spans[b].Offset) }
	steps := make([]ui.WaterfallStep, 0, len(tr.Spans))
	seen := make([]bool, len(tr.Spans))
	var walk func(idx []int, depth int)
	walk = func(idx []int, depth int) {
		slices.SortStableFunc(idx, byStart)
		for _, i := range idx {
			if seen[i] {
				continue // a parent loop in the data: draw each span once
			}
			seen[i] = true
			sp := tr.Spans[i]
			step := ui.WaterfallStep{Label: sp.Name, Start: sp.Offset, Dur: sp.Dur, Depth: depth}
			// The request itself reads as its route: the span's own
			// name says the method twice ("HTTP GET GET /orders/{id}").
			if depth == 0 && len(roots) == 1 && tr.Route != "" {
				step.Label = tr.Route
			}
			if step.Label == "" {
				step.Label = sp.SpanID
			}
			if step.Label == "" {
				step.Label = "?"
			}
			if sp.Error {
				step.Tone = ui.WaterfallDanger
			}
			for _, want := range detailAttrs {
				if j := slices.IndexFunc(sp.Attrs, func(a itel.Attr) bool { return a.Key == want }); j >= 0 {
					step.Detail = sp.Attrs[j].Value
					break
				}
			}
			steps = append(steps, step)
			walk(children[sp.SpanID], depth+1)
		}
	}
	walk(roots, 0)
	return steps
}

// ----- logs --------------------------------------------------------------------

// level is one choice of minimum level.
type level struct {
	key   string
	label i18n.Phrase
	min   slog.Level
}

var levels = []level{
	{"", i18nui.TelemetryLevelAll, slog.LevelDebug - 100},
	{"info", i18nui.TelemetryLevelInfo, slog.LevelInfo},
	{"warn", i18nui.TelemetryLevelWarn, slog.LevelWarn},
	{"error", i18nui.TelemetryLevelError, slog.LevelError},
}

// Logs draws the log, newest first, narrowed by level, text and time.
func (s *Screens) Logs(ctx context.Context) render.HTML {
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryLogs.Text(ctx),
		Subtitle: i18nui.TelemetryLogsSub.Text(ctx),
	})
	store := s.tel.Reader()
	if store == nil {
		return page(s.tabNav(ctx, true), header, problem(ctx, i18nui.TelemetryNotReady))
	}
	q := query(ctx)
	text := strings.TrimSpace(param(q, "q"))
	lv := levels[0]
	for _, l := range levels {
		if l.key == param(q, "level") {
			lv = l
		}
	}
	win, from := s.pickWindow(q)
	cursor := before(q)
	lines, err := store.Logs(itel.LogQuery{Text: text, MinLevel: lv.min, From: from, To: cursor}, logsPerPage)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: list logs", "err", err)
		return page(s.tabNav(ctx, true), header, problem(ctx, i18nui.TelemetryLoadFailed))
	}

	path := s.LogsPath()
	here := func(level, before string) string {
		w := win.key
		if w == defaultWindow {
			w = ""
		}
		return link(path, "level", level, "q", text, "window", w, "before", before)
	}
	items := make([]ui.SegmentLink, len(levels))
	for i, l := range levels {
		items[i] = ui.SegmentLink{Text: l.label.Text(ctx), Href: here(l.key, ""), Current: l.key == lv.key}
	}
	segments := ui.SegmentedLinks(ui.SegmentedLinksConfig{Label: i18nui.TelemetryLevel.Text(ctx), Items: items, Ctx: ctx})
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: path, ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Search:     &ui.FilterSearch{Name: "q", Value: text, Label: i18nui.TelemetrySearchLogs.Text(ctx), Placeholder: i18nui.TelemetrySearchHint.Text(ctx)},
		Facets:     []ui.Facet{s.windowFacet(ctx, win)},
		Hidden:     []ui.HiddenField{{Name: "level", Value: lv.key}},
		HideReset:  true,
	})
	var next string
	if len(lines) == logsPerPage {
		next = here(lv.key, lines[len(lines)-1].Time.Add(-time.Nanosecond).UTC().Format(time.RFC3339Nano))
	}
	return page(s.tabNav(ctx, true), header, segments, filters,
		s.logTable(ctx, lines, true, i18nui.TelemetryLogsEmpty, i18nui.TelemetryLogsEmptySub),
		older(ctx, next != "", next, here(lv.key, ""), !cursor.IsZero()))
}

// logTable draws log lines. linkRequests adds the column that opens a
// line's request, which a request's own page has no use for.
func (s *Screens) logTable(ctx context.Context, lines []itel.LogEntry, linkRequests bool, empty, emptySub i18n.Phrase) render.HTML {
	now := s.now()
	cols := []ui.Column{
		{Key: "time", Header: i18nui.TelemetryColTime.Text(ctx), Phone: ui.PhoneSubtitle},
		{Key: "level", Header: i18nui.TelemetryColLevel.Text(ctx), Phone: ui.PhoneMeta},
		{Key: "msg", Header: i18nui.TelemetryColMessage.Text(ctx), Phone: ui.PhoneTitle, Wrap: true},
		// A request's own page has room for every attribute; the long
		// list keeps one line a row.
		{Key: "details", Header: i18nui.TelemetryColDetails.Text(ctx), Truncate: linkRequests, Wrap: !linkRequests},
	}
	if linkRequests {
		cols = append(cols, ui.Column{Key: "request", Header: i18nui.TelemetryColRequest.Text(ctx), Align: "end", Fit: true, Phone: ui.PhoneEnd})
	}
	rows := make([]ui.Row, len(lines))
	for i, e := range lines {
		f := readLine(e.Line)
		details := ui.EmptyValue()
		if f.details != "" {
			details = ui.InlineCode(f.details)
		}
		cells := map[string]render.HTML{
			"time":    timeCell(ctx, now, e.Time),
			"level":   levelBadge(e.Level),
			"msg":     render.Text(f.msg),
			"details": details,
		}
		if linkRequests {
			cells["request"] = ui.EmptyValue()
			if f.traceID != "" {
				cells["request"] = ui.Link(ui.LinkConfig{Href: link(s.RequestsPath(), "trace", f.traceID), Text: i18nui.TelemetryOpenTrace.Text(ctx), Variant: ui.LinkAction,
					ExtraAttrs: html.Attrs{"aria-label": i18nui.TelemetryOpenTraceOf.Text(ctx, map[string]any{
						"route": f.msg, "time": i18n.DateTime(ctx, e.Time.UTC(), i18n.DateMedium, i18n.TimeLong)})}})
			}
		}
		rows[i] = ui.Row{ID: "l" + strconv.Itoa(i), Cells: cells}
	}
	es := ui.EmptyStateConfig{Title: empty.Text(ctx), HeadingLevel: 3}
	if emptySub != (i18n.Phrase{}) {
		es.Description = emptySub.Text(ctx)
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns: cols, Rows: rows, Ctx: ctx,
		Caption: i18nui.TelemetryLogsCaption.Text(ctx), CaptionHidden: true,
		Responsive: ui.ResponsiveRows, Empty: es,
	})
}

// levelBadge shows a level by slog's own name for it (INFO, WARN,
// ERROR+2): the word an operator searches the files for.
func levelBadge(l slog.Level) render.HTML {
	variant := ui.StatusNeutral
	switch {
	case l >= slog.LevelError:
		variant = ui.StatusDanger
	case l >= slog.LevelWarn:
		variant = ui.StatusWarning
	case l >= slog.LevelInfo:
		variant = ui.StatusInfo
	}
	return ui.StatusBadge(ui.StatusBadgeConfig{Label: l.String(), Variant: variant})
}

// lineFields is what a table row shows of a log line.
type lineFields struct {
	msg, traceID, details string
}

// maxDetails bounds the attributes written beside a message.
const maxDetails = 400

// firstDetails are the attributes written ahead of the rest, in this
// order: what went wrong, then what was asked and how it went.
var firstDetails = []string{"err", "error", "panic", "status", "dur_ms", "method", "path", "route"}

// readLine splits a log line into its message, its trace ID and the
// rest of its attributes as "key=value" pairs: the ones in firstDetails
// first, then the others in key order, empty ones left out. An access
// line's message is the request it records ("GET /pricing"), since
// "http.access" on every row says nothing. A line that is not a JSON
// object shows whole as its message.
func readLine(line []byte) lineFields {
	var m map[string]any
	if err := jsonexact.Unmarshal(line, &m); err != nil {
		return lineFields{msg: string(line[:min(len(line), maxDetails)])}
	}
	f := lineFields{}
	f.msg, _ = m["msg"].(string)
	f.traceID, _ = m["trace_id"].(string)
	if len(f.traceID) != 32 {
		f.traceID = ""
	}
	skip := map[string]bool{"time": true, "level": true, "msg": true, "trace_id": true, "span_id": true}
	if f.msg == "http.access" {
		method, _ := m["method"].(string)
		path, _ := m["path"].(string)
		if method != "" && path != "" {
			f.msg = method + " " + path
			skip["method"], skip["path"] = true, true
			// The status rides with the message: a phone row shows
			// no details.
			if status := scalar(m["status"]); status != "" {
				f.msg += " → " + status
				skip["status"] = true
			}
		}
	}
	keys := make([]string, 0, len(m))
	for _, k := range firstDetails {
		if _, ok := m[k]; ok && !skip[k] {
			keys = append(keys, k)
			skip[k] = true
		}
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !skip[k] {
			keys = append(keys, k)
		}
	}
	var sb strings.Builder
	for _, k := range keys {
		val := scalar(m[k])
		if val == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(val)
		if sb.Len() > maxDetails {
			break
		}
	}
	f.details = sb.String()
	if len(f.details) > maxDetails {
		f.details = strings.ToValidUTF8(f.details[:maxDetails], "") + "…"
	}
	return f
}

// scalar writes one decoded JSON value as text: a string as itself, a
// number or a boolean as written, an object or an array as JSON, and
// null as nothing.
func scalar(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any, []any:
		b, err := json.Marshal(v)
		if err != nil {
			return "?"
		}
		return string(b)
	default:
		return fmt.Sprint(v)
	}
}
