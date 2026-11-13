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
	requestsPerPage = 25
	logsPerPage     = 25
	maxParam        = 256
)

// Screens draws the pages for one telemetry plugin at one place in the
// app's URLs.
type Screens struct {
	tel  *telemetry.Plugin
	base string
	tabs bool
	now  func() time.Time
	// searchBudget is how long a log search may read before it shows
	// what it has and offers to carry on.
	searchBudget time.Duration
	// firingNow returns the alert rules firing now: the plugin's.
	firingNow func() []telemetry.Alert

	// Live, when set, returns the URL that serves a list's rows alone
	// ("requests" or "logs", drawn by RequestsRows and LogsRows). The
	// newest page of each list then re-fetches its rows on a timer.
	// The host that sets it serves that URL behind the same gate as
	// the pages.
	Live func(list string) string
}

// liveEvery is how often the newest page of a list re-fetches its rows.
const liveEvery = 10 * time.Second

// rows wraps a list's table and pager. On the newest page, with a host
// that serves the rows alone, the wrapper re-fetches them on a timer:
// a page someone has paged back on stays as it is.
func (s *Screens) rows(list string, cur cursor, src string, parts ...render.HTML) render.HTML {
	body := page(parts...)
	if s.Live == nil || cur.set() {
		return body
	}
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
		"data-cui-poll":     liveEvery.String(),
		"data-cui-poll-src": src,
	}}, body)
}

// liveBadge says a list keeps itself current, when it does.
func (s *Screens) liveBadge(ctx context.Context, cur cursor) render.HTML {
	if s.Live == nil || cur.set() {
		return ""
	}
	return ui.PollingIndicator(ui.PollingIndicatorConfig{Ctx: ctx})
}

// NewScreens returns the screens for tel, linking to each other under
// base ("/telemetry", "/admin/telemetry"). With tabs, each page draws
// the links between the two itself, for a host with no navigation of
// its own.
func NewScreens(tel *telemetry.Plugin, base string, tabs bool) *Screens {
	return &Screens{tel: tel, base: strings.TrimRight(base, "/"), tabs: tabs, now: time.Now, searchBudget: 3 * time.Second, firingNow: tel.Firing}
}

// RequestsPath, IssuesPath and LogsPath are where the host serves the
// three pages.
func (s *Screens) RequestsPath() string { return s.base + "/requests" }

// IssuesPath is where the host serves the issues page.
func (s *Screens) IssuesPath() string { return s.base + "/issues" }

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

// cursor is where a page sits in the list: before a time (older rows),
// after one (newer rows), or neither, which is the newest page.
type cursor struct{ before, after time.Time }

func (c cursor) set() bool { return !c.before.IsZero() || !c.after.IsZero() }

func readCursor(q url.Values) cursor {
	var c cursor
	if t, err := time.Parse(time.RFC3339Nano, param(q, "before")); err == nil {
		c.before = t
	}
	// One at a time: a URL carrying both is read as the older page.
	if t, err := time.Parse(time.RFC3339Nano, param(q, "after")); err == nil && c.before.IsZero() {
		c.after = t
	}
	return c
}

// bounds narrows a window to the cursor's page and says which end the
// limit is taken from.
func (c cursor) bounds(from time.Time) (lo, hi time.Time, oldestFirst bool) {
	lo, hi = from, c.before
	if !c.after.IsZero() {
		if c.after.After(lo) {
			lo = c.after
		}
		oldestFirst = true
	}
	return lo, hi, oldestFirst
}

func stampParam(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// trim cuts a page fetched one row over its limit back to the limit and
// reports whether pages exist on each side. The extra row is the proof
// of another page: the oldest row when the limit was taken from the new
// end, the newest when it was taken from the old end (a "newer" page).
// A page reached by a cursor came from the other side, so that side has
// one too.
func trim[T any](c cursor, rows []T, limit int) (page []T, hasNewer, hasOlder bool) {
	extra := len(rows) > limit
	switch {
	case !c.after.IsZero():
		if extra {
			rows = rows[len(rows)-limit:]
		}
		return rows, extra, true
	default:
		if extra {
			rows = rows[:limit]
		}
		return rows, !c.before.IsZero(), extra
	}
}

// pager draws the links to the neighbouring pages and says what this
// one holds. newerAt and olderAt are where those pages start, zero for
// none; newest and oldest are the first and last rows' times and n how
// many rows there are. here builds this list's URL with the given
// before and after values.
func pager(ctx context.Context, c cursor, n int, newerAt, olderAt, newest, oldest time.Time, here func(before, after string) string) render.HTML {
	if !c.set() && olderAt.IsZero() && newerAt.IsZero() {
		return "" // everything fits on the one page
	}
	link := func(label i18n.Phrase, href string, on bool) render.HTML {
		if !on {
			return ui.Button(ui.ButtonConfig{Ctx: ctx, Label: label.Text(ctx), Variant: ui.ButtonSecondary, Disabled: true})
		}
		return ui.LinkButton(ui.LinkButtonConfig{Label: label.Text(ctx), Href: href, Variant: ui.ButtonSecondary})
	}
	links := []render.HTML{}
	if c.set() {
		links = append(links, ui.LinkButton(ui.LinkButtonConfig{Label: i18nui.TelemetryNewest.Text(ctx), Href: here("", ""), Variant: ui.ButtonGhost}))
	}
	links = append(links,
		link(i18nui.TelemetryNewer, here("", stampParam(newerAt)), !newerAt.IsZero()),
		link(i18nui.TelemetryOlder, here(stampParam(olderAt), ""), !olderAt.IsZero()))
	var summary render.HTML
	if n > 0 {
		summary = ui.Muted(render.Text(i18nui.TelemetryPageRange.Text(ctx, map[string]any{
			"n":    i18n.Number(ctx, n),
			"from": i18n.DateTime(ctx, oldest.UTC(), i18n.DateMedium, i18n.TimeLong),
			"to":   i18n.DateTime(ctx, newest.UTC(), i18n.DateMedium, i18n.TimeLong),
		})))
	}
	return html.Nav(html.NavConfig{Label: i18nui.TelemetryPaging.Text(ctx)},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Justify: ui.JustifyBetween, Align: ui.AlignCenter},
			summary, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, links...)))
}

// neighbours turns a page's rows and flags into where its neighbouring
// pages start: just past its newest and oldest rows.
func neighbours(n int, hasNewer, hasOlder bool, newest, oldest time.Time) (newerAt, olderAt time.Time) {
	if n > 0 && hasNewer {
		newerAt = newest.Add(time.Nanosecond)
	}
	if n > 0 && hasOlder {
		olderAt = oldest.Add(-time.Nanosecond)
	}
	return newerAt, olderAt
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

// The three pages, as tabNav names the current one.
const (
	tabOverview = iota
	tabRequests
	tabIssues
	tabLogs
)

func (s *Screens) tabNav(ctx context.Context, current int) render.HTML {
	if !s.tabs {
		return ""
	}
	return ui.TabNav(ui.TabNavConfig{
		Label: i18nui.TelemetryNav.Text(ctx),
		Items: []ui.TabNavItem{
			{Text: i18nui.TelemetryOverview.Text(ctx), Href: s.OverviewPath(), Current: current == tabOverview},
			{Text: i18nui.TelemetryRequests.Text(ctx), Href: s.RequestsPath(), Current: current == tabRequests},
			{Text: i18nui.TelemetryIssues.Text(ctx), Href: s.IssuesPath(), Current: current == tabIssues},
			{Text: i18nui.TelemetryLogs.Text(ctx), Href: s.LogsPath(), Current: current == tabLogs},
		},
	})
}

// firing is the notice of the alert rules firing now, for the top of
// the requests and issues pages: the place someone told of trouble
// lands on.
func (s *Screens) firing(ctx context.Context) render.HTML {
	alerts := s.firingNow()
	if len(alerts) == 0 {
		return ""
	}
	lines := make([]render.HTML, len(alerts))
	for i, a := range alerts {
		phrase := i18nui.TelemetryAlertFailed
		if a.Rule.Kind == telemetry.AlertSlow {
			phrase = i18nui.TelemetryAlertSlow
		}
		within := a.Rule.Within
		if within == 0 {
			within = 5 * time.Minute
		}
		lines[i] = render.Text(phrase.Text(ctx, map[string]any{
			"name": a.Rule.Name, "count": i18n.Number(ctx, a.Count), "minutes": i18n.Number(ctx, int(within/time.Minute))}))
	}
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, ui.Stack(ui.StackConfig{Gap: ui.GapSM}, lines...))
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
	u, today := t.UTC(), now.UTC()
	full := i18n.DateTime(ctx, u, i18n.DateMedium, i18n.TimeLong)
	// Today's rows say the time alone: the date on every row is noise,
	// and a phone row has no room for it.
	shown := full
	if u.Year() == today.Year() && u.YearDay() == today.YearDay() {
		shown = i18n.Time(ctx, u, i18n.TimeLong)
	}
	return html.Time(html.TimeConfig{Datetime: u.Format(time.RFC3339Nano),
		ExtraAttrs: html.Attrs{"title": full + " (" + ui.Ago(ctx, now, t) + ")"}},
		render.Text(shown))
}

func (s *Screens) windowFacet(ctx context.Context, current window) ui.Facet {
	f := ui.Facet{Name: "window", Label: i18nui.TelemetryWindow.Text(ctx), Value: current.key}
	for _, w := range windows {
		f.Options = append(f.Options, ui.FacetOption{Label: w.label.Text(ctx), Value: w.key})
	}
	return f
}

// ----- requests ----------------------------------------------------------------

// Requests draws the list of kept requests, or one request when the
// query names a trace.
func (s *Screens) Requests(ctx context.Context) render.HTML { return s.requests(ctx, false) }

// RequestsRows draws the requests list's table and pager alone, for the
// same query: what the newest page re-fetches.
func (s *Screens) RequestsRows(ctx context.Context) render.HTML { return s.requests(ctx, true) }

func (s *Screens) requests(ctx context.Context, rowsOnly bool) render.HTML {
	q := query(ctx)
	if id := strings.ToLower(strings.TrimSpace(param(q, "trace"))); id != "" {
		if rowsOnly {
			return ""
		}
		return s.request(ctx, id)
	}
	store := s.tel.Reader()
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryRequests.Text(ctx),
		Subtitle: i18nui.TelemetryRequestsSub.Text(ctx),
	})
	if store == nil {
		return page(s.tabNav(ctx, tabRequests), header, problem(ctx, i18nui.TelemetryNotReady))
	}

	show, route, issue := param(q, "show"), param(q, "route"), param(q, "issue")
	win, from := s.pickWindow(q)
	cur := readCursor(q)
	lo, hi, oldestFirst := cur.bounds(from)
	tq := itel.TraceQuery{Route: route, Issue: issue, From: lo, To: hi, OldestFirst: oldestFirst}
	switch show {
	case "failed":
		tq.Failed = true
	case "slow":
		tq.MinDur = s.tel.SlowThreshold()
	default:
		show = ""
	}
	traces, err := store.Traces(tq, requestsPerPage+1)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: list requests", "err", err)
		return page(s.tabNav(ctx, tabRequests), header, problem(ctx, i18nui.TelemetryLoadFailed))
	}
	traces, hasNewer, hasOlder := trim(cur, traces, requestsPerPage)

	path := s.RequestsPath()
	wkey := win.key
	if wkey == defaultWindow {
		wkey = ""
	}
	here := func(show, route string) string {
		return link(path, "show", show, "route", route, "issue", issue, "window", wkey)
	}
	segments := ui.SegmentedLinks(ui.SegmentedLinksConfig{
		Label: i18nui.TelemetryShow.Text(ctx), Ctx: ctx,
		Items: []ui.SegmentLink{
			{Text: i18nui.TelemetryShowAll.Text(ctx), Href: here("", route), Current: show == ""},
			{Text: i18nui.TelemetryShowFailed.Text(ctx), Href: here("failed", route), Current: show == "failed"},
			{Text: i18nui.TelemetryShowSlow.Text(ctx), Href: here("slow", route), Current: show == "slow"},
		},
	})
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: path, ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Search:     &ui.FilterSearch{Name: "trace", Label: i18nui.TelemetryFindTrace.Text(ctx), Placeholder: i18nui.TelemetryTraceIDHint.Text(ctx)},
		Facets:     []ui.Facet{s.windowFacet(ctx, win)},
		Hidden:     []ui.HiddenField{{Name: "show", Value: show}, {Name: "route", Value: route}, {Name: "issue", Value: issue}},
		HideReset:  true,
	})
	var routeChip render.HTML
	if route != "" {
		routeChip = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
			ui.Tag(ui.TagConfig{Ctx: ctx, Label: i18nui.TelemetryRouteFilter.Text(ctx, map[string]any{"route": route})}),
			ui.Link(ui.LinkConfig{Href: here(show, ""), Text: i18nui.TelemetryRouteClear.Text(ctx), Variant: ui.LinkMuted}))
	}

	if issue != "" {
		routeChip = page(routeChip, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
			ui.Tag(ui.TagConfig{Ctx: ctx, Label: i18nui.TelemetryIssueFilter.Text(ctx), Variant: ui.StatusInfo}),
			ui.Link(ui.LinkConfig{Href: link(path, "show", show, "route", route, "window", wkey), Text: i18nui.TelemetryIssueClear.Text(ctx)})))
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
			routeCell = ui.Link(ui.LinkConfig{Href: here(show, t.Route), Text: t.Route,
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
	var newest, oldest time.Time
	if len(traces) > 0 {
		newest, oldest = traces[0].Time, traces[len(traces)-1].Time
	}
	newerAt, olderAt := neighbours(len(traces), hasNewer, hasOlder, newest, oldest)
	pages := pager(ctx, cur, len(traces), newerAt, olderAt, newest, oldest, func(before, after string) string {
		return link(path, "show", show, "route", route, "issue", issue, "window", wkey, "before", before, "after", after)
	})
	if rowsOnly {
		return page(table, pages)
	}
	var src string
	if s.Live != nil {
		src = link(s.Live("requests"), "show", show, "route", route, "issue", issue, "window", wkey)
	}
	header = ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryRequests.Text(ctx),
		Subtitle: i18nui.TelemetryRequestsSub.Text(ctx),
		Actions:  s.liveBadge(ctx, cur),
	})
	return page(s.tabNav(ctx, tabRequests), header, s.firing(ctx), segments, filters, routeChip, s.rows("requests", cur, src, table, pages))
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
		return page(s.tabNav(ctx, tabRequests),
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
	return page(s.tabNav(ctx, tabRequests), header, summary,
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

// ----- issues ------------------------------------------------------------------

// issuesShown is the most issues one page lists: the latest to be seen.
const issuesShown = 100

// Issues draws the failed requests of a time range grouped by what went
// wrong: how many times, since when, and the way to the requests.
func (s *Screens) Issues(ctx context.Context) render.HTML {
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryIssues.Text(ctx),
		Subtitle: i18nui.TelemetryIssuesSub.Text(ctx),
	})
	store := s.tel.Reader()
	if store == nil {
		return page(s.tabNav(ctx, tabIssues), header, problem(ctx, i18nui.TelemetryNotReady))
	}
	q := query(ctx)
	win, from := s.pickWindow(q)
	issues, leftOut, err := store.Issues(from, time.Time{}, issuesShown)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: list issues", "err", err)
		return page(s.tabNav(ctx, tabIssues), header, problem(ctx, i18nui.TelemetryLoadFailed))
	}
	wkey := win.key
	if wkey == defaultWindow {
		wkey = ""
	}
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: s.IssuesPath(), ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Facets:     []ui.Facet{s.windowFacet(ctx, win)},
		HideReset:  true,
	})
	now := s.now()
	cols := []ui.Column{
		// The error and its route are separate columns so the phone row
		// can end a long error with an ellipsis: a stacked cell is one
		// box the cell's ellipsis cannot reach, and the text was cut
		// mid-word.
		{Key: "error", Header: i18nui.TelemetryColError.Text(ctx), Phone: ui.PhoneTitle, Wrap: true},
		{Key: "route", Header: i18nui.TelemetryColRoute.Text(ctx), Phone: ui.PhoneSubtitle},
		{Key: "status", Header: i18nui.TelemetryColStatus.Text(ctx)},
		{Key: "count", Header: i18nui.TelemetryColCount.Text(ctx), Align: "end", Phone: ui.PhoneMeta},
		{Key: "last", Header: i18nui.TelemetryColLastSeen.Text(ctx)},
		{Key: "first", Header: i18nui.TelemetryColFirstSeen.Text(ctx)},
		{Key: "open", Header: i18nui.TelemetryColRequest.Text(ctx), Align: "end", Fit: true, Phone: ui.PhoneEnd},
	}
	rows := make([]ui.Row, len(issues))
	for i, is := range issues {
		what := is.Err
		if what == "" {
			what = i18nui.TelemetryIssueNoError.Text(ctx)
		}
		rows[i] = ui.Row{ID: is.ID, Cells: map[string]render.HTML{
			"error":  render.Text(what),
			"route":  render.Text(is.Route),
			"status": statusBadge(ctx, is.Status, true, 0),
			"count":  render.Text(i18n.Number(ctx, is.Count)),
			"last":   timeCell(ctx, now, is.Last),
			"first":  timeCell(ctx, now, is.First),
			"open": ui.Link(ui.LinkConfig{Href: link(s.RequestsPath(), "show", "failed", "issue", is.ID, "window", wkey),
				Text: i18nui.TelemetryIssueRequests.Text(ctx), Variant: ui.LinkAction,
				ExtraAttrs: html.Attrs{"aria-label": i18nui.TelemetryIssueRequestsOf.Text(ctx, map[string]any{
					"count": i18n.Number(ctx, is.Count), "error": what})}}),
		}}
	}
	var note render.HTML
	if leftOut > 0 {
		note = ui.Callout(ui.CalloutConfig{Variant: ui.StatusWarning}, render.Text(i18nui.TelemetryIssuesLeftOut.Text(ctx, map[string]any{"n": i18n.Number(ctx, leftOut)})))
	}
	table := ui.DataTable(ui.DataTableConfig{
		Columns: cols, Rows: rows, Ctx: ctx,
		Caption: i18nui.TelemetryIssues.Text(ctx), CaptionHidden: true,
		Responsive: ui.ResponsiveRows,
		Empty: ui.EmptyStateConfig{
			Title:       i18nui.TelemetryIssuesEmpty.Text(ctx),
			Description: i18nui.TelemetryIssuesEmptySub.Text(ctx),
		},
	})
	return page(s.tabNav(ctx, tabIssues), header, s.firing(ctx), filters, note, table)
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
func (s *Screens) Logs(ctx context.Context) render.HTML { return s.logs(ctx, false) }

// LogsRows draws the log's table and pager alone, for the same query:
// what the newest page re-fetches.
func (s *Screens) LogsRows(ctx context.Context) render.HTML { return s.logs(ctx, true) }

func (s *Screens) logs(ctx context.Context, rowsOnly bool) render.HTML {
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryLogs.Text(ctx),
		Subtitle: i18nui.TelemetryLogsSub.Text(ctx),
	})
	store := s.tel.Reader()
	if store == nil {
		return page(s.tabNav(ctx, tabLogs), header, problem(ctx, i18nui.TelemetryNotReady))
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
	cur := readCursor(q)
	lo, hi, oldestFirst := cur.bounds(from)
	// Access lines for scripts and styles would fill every page, so
	// they are left out unless asked for.
	all := param(q, "all")
	if all != "1" {
		all = ""
	}
	lq := itel.LogQuery{Text: text, MinLevel: lv.min, From: lo, To: hi, OldestFirst: oldestFirst}
	if all == "" {
		lq.SkipPaths = s.tel.QuietPaths()
	}
	lines, stopped, err := store.LogsWithin(lq, logsPerPage+1, s.searchBudget)
	if err != nil {
		slog.ErrorContext(ctx, "telemetry: list logs", "err", err)
		return page(s.tabNav(ctx, tabLogs), header, problem(ctx, i18nui.TelemetryLoadFailed))
	}
	lines, hasNewer, hasOlder := trim(cur, lines, logsPerPage)

	path := s.LogsPath()
	wkey := win.key
	if wkey == defaultWindow {
		wkey = ""
	}
	here := func(level string) string { return link(path, "level", level, "q", text, "window", wkey, "all", all) }
	quietNote, quietLink, quietHref := i18nui.TelemetryQuietHidden, i18nui.TelemetryQuietShow, link(path, "level", lv.key, "q", text, "window", wkey, "all", "1")
	if all != "" {
		quietNote, quietLink, quietHref = i18nui.TelemetryQuietShown, i18nui.TelemetryQuietHide, link(path, "level", lv.key, "q", text, "window", wkey)
	}
	quiet := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		ui.Muted(render.Text(quietNote.Text(ctx))),
		ui.Link(ui.LinkConfig{Href: quietHref, Text: quietLink.Text(ctx)}))
	items := make([]ui.SegmentLink, len(levels))
	for i, l := range levels {
		items[i] = ui.SegmentLink{Text: l.label.Text(ctx), Href: here(l.key), Current: l.key == lv.key}
	}
	segments := ui.SegmentedLinks(ui.SegmentedLinksConfig{Label: i18nui.TelemetryLevel.Text(ctx), Items: items, Ctx: ctx})
	filters := ui.FilterToolbar(ui.FilterToolbarConfig{
		Action: path, ClientNavigation: true, Ctx: ctx,
		Label:      i18nui.TelemetryFilters.Text(ctx),
		ApplyLabel: i18nui.TelemetryApply.Text(ctx),
		Search:     &ui.FilterSearch{Name: "q", Value: text, Label: i18nui.TelemetrySearchLogs.Text(ctx), Placeholder: i18nui.TelemetrySearchHint.Text(ctx)},
		Facets:     []ui.Facet{s.windowFacet(ctx, win)},
		Hidden:     []ui.HiddenField{{Name: "level", Value: lv.key}, {Name: "all", Value: all}},
		HideReset:  true,
	})
	var newest, oldest time.Time
	if len(lines) > 0 {
		newest, oldest = lines[0].Time, lines[len(lines)-1].Time
	}
	table := s.logTable(ctx, lines, true, i18nui.TelemetryLogsEmpty, i18nui.TelemetryLogsEmptySub)
	newerAt, olderAt := neighbours(len(lines), hasNewer, hasOlder, newest, oldest)
	// A search that ran out of time carries on from where it stopped,
	// in the direction it was reading, whether or not it found a row.
	var stoppedNote render.HTML
	if !stopped.IsZero() {
		if oldestFirst {
			newerAt = stopped
		} else {
			olderAt = stopped
		}
		stoppedNote = ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo}, render.Text(i18nui.TelemetrySearchStopped.Text(ctx, map[string]any{
			"time": i18n.DateTime(ctx, stopped.UTC(), i18n.DateMedium, i18n.TimeLong)})))
	}
	pages := pager(ctx, cur, len(lines), newerAt, olderAt, newest, oldest, func(before, after string) string {
		return link(path, "level", lv.key, "q", text, "window", wkey, "all", all, "before", before, "after", after)
	})
	if rowsOnly {
		return page(stoppedNote, table, pages)
	}
	var src string
	if s.Live != nil {
		src = link(s.Live("logs"), "level", lv.key, "q", text, "window", wkey, "all", all)
	}
	header = ui.PageHeader(ui.PageHeaderConfig{
		Title:    i18nui.TelemetryLogs.Text(ctx),
		Subtitle: i18nui.TelemetryLogsSub.Text(ctx),
		Actions:  s.liveBadge(ctx, cur),
	})
	return page(s.tabNav(ctx, tabLogs), header, segments, filters, quiet, s.rows("logs", cur, src, stoppedNote, table, pages))
}

// logTable draws log lines. linkRequests adds the column that opens a
// line's request, which a request's own page has no use for.
func (s *Screens) logTable(ctx context.Context, lines []itel.LogEntry, linkRequests bool, empty, emptySub i18n.Phrase) render.HTML {
	now := s.now()
	cols := []ui.Column{
		// On a phone: the message over its details, the level over the
		// time at the end.
		{Key: "time", Header: i18nui.TelemetryColTime.Text(ctx), Phone: ui.PhoneDetail},
		{Key: "level", Header: i18nui.TelemetryColLevel.Text(ctx), Phone: ui.PhoneMeta},
		{Key: "msg", Header: i18nui.TelemetryColMessage.Text(ctx), Phone: ui.PhoneTitle, Wrap: true},
		// A request's own page has room for every attribute; the long
		// list keeps one line a row.
		{Key: "details", Header: i18nui.TelemetryColDetails.Text(ctx), Truncate: linkRequests, Wrap: !linkRequests, Phone: ui.PhoneSubtitle},
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
