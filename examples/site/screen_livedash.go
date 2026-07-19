package main

// ── Live dashboard reference (additive — /examples/live-dashboard) ───
//
// The canonical end-to-end composition of GoFastr's live-data primitives.
// It builds a realistic ops dashboard (queue throughput + job status) by
// COMPOSING existing framework/ui + core-ui components — no bespoke CSS,
// no inline styles, no new runtime JS. The point is the reference for
// how the primitives fit together, documented in
// framework/docs/content/live-dashboards.md.
//
// What it demonstrates, mapped to the issue's acceptance list:
//
//   - Several live numeric StatCards fed by SSE island push. A ticker in
//     setupServer advances the demo state and calls host.Islands.PushUpdate
//     with fresh HTML for the "livedash-stats" region every tick. The
//     runtime swaps just that region's innerHTML — no full reload, no
//     re-render of the rest of the page.
//   - A COMPUTED signal. dash.status (store.Computed) derives an
//     operational-status label from dash.incidentsOpen and
//     dash.incidentsAckd. The operator console's +/- buttons mutate the
//     dependencies purely client-side; the reducer (registered via
//     WithExtraScripts, CSP-safe) runs in the browser and the bound
//     StatusPill updates live. This is the one place signals are used —
//     the metric StatCards are NOT signal-bound, because high-frequency
//     visual updates should not be routed through an aria-live region.
//   - A bounded activity feed. The Timeline lives in a data-island slot
//     with role="status" (polite aria-live by ARIA convention). The
//     server trims the feed to liveDashFeedCap entries before each
//     re-render — the client never buffers or trims.
//   - A keyed changing collection. The jobs DataTable renders one row
//     per job; Row.ID is the job key, so successive pushes produce
//     near-identical HTML that differs only on changed rows.
//   - Connection health/retry. ui.NetworkRetryBanner watches the SSE
//     lane: SSESilenceMs trips the banner if the ticker goes quiet, and
//     the Retry button probes /__site/livedash/health.
//   - Topic-scoped delivery. Pushes are addressed to
//     host.Islands.PresenceSessions(liveDashTopic) — only sessions that
//     joined the "live-dashboard-demo" presence topic receive them. The
//     page is linked with ?presence=live-dashboard-demo so the SSE
//     connection joins the topic. To isolate tenants on an authenticated
//     app, use a server-derived tenant-qualified topic
//     (tenant:<id>:dashboard) and render per-tenant state — a single fixed
//     topic + AuthorizeTopic authorizes the join but does NOT scope the
//     broadcast, so every viewer would get the same push. See
//     docs/live-dashboards "Tenant isolation".
//   - Reconnect + authoritative refresh. SSE is best-effort and lossy:
//     a dropped frame is gone. The GET /__site/livedash/refresh endpoint
//     returns the current rendered island HTML so an app can reconcile
//     on SSE reconnect. The runtime does NOT do this for you — the doc
//     shows the one-line listener an app adds.
//
// The single-replica demo state lives in the package-level liveDash
// value; the ticker advances it under a mutex and pushes snapshots.

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	// liveDashTopic is the presence topic the dashboard joins. Any SSE
	// connection carrying ?presence=live-dashboard-demo is a push target.
	// The page is always linked with this topic so viewers see live data.
	liveDashTopic = "live-dashboard-demo"

	// Island slot IDs. The data-island attribute on each region matches
	// the IslandID the ticker pushes to. Stable across SSR + push so the
	// runtime swaps the right slot.
	liveDashStatsID = "livedash-stats"
	liveDashFeedID  = "livedash-feed"
	liveDashJobsID  = "livedash-jobs"

	// liveDashFeedCap is the max entries the server keeps in the activity
	// feed. Trimming happens server-side before each re-render — the
	// client never buffers.
	liveDashFeedCap = 8

	// liveDashSeriesCap is the max points retained for the throughput
	// Sparkline. Older points drop as new ones arrive.
	liveDashSeriesCap = 16
)

// dash is the typed signal store for the operator-console region of the
// dashboard. These are PAGE-SCOPED client signals — they reset on
// navigation, which is correct for ephemeral "what am I declaring right
// now" state. The metric StatCards are NOT bound to signals; they live
// behind SSE island push because they are high-frequency background
// values, not user state.
var dash = store.New("dash")

var (
	dashIncidentsOpen = dash.Int("incidentsOpen", 0)
	dashIncidentsAckd = dash.Int("incidentsAckd", 0)
	// dashStatus is a store.Computed slice: when either dependency
	// changes client-side, the runtime runs the "dash.status" reducer
	// (registered via uihost.WithExtraScripts in setupServer) and fans
	// the result to every consumer. The reducer is a real JS function —
	// no eval, CSP-safe.
	dashStatus = store.Computed[string](dash, "status", "dash.status",
		"dash.incidentsOpen", "dash.incidentsAckd")
)

// liveDashJob is one row in the jobs DataTable.
type liveDashJob struct {
	ID       string // stable row key — Row.ID
	Name     string
	Status   string // running | done | queued | failed
	Progress int    // 0–100
}

// liveDashData is the lock-free mutable payload. Renderers take it by
// value so a snapshot is fully decoupled from the mutex holder — no
// accidental lock copy, no serializing all renders through one mutex.
type liveDashData struct {
	throughput int64     // events/sec
	workers    int64     // active workers
	depth      int64     // queue depth (pending events)
	p99        int64     // latency p99 in ms
	ok         int64     // successful jobs in the current window
	total      int64     // total jobs in the current window
	series     []float64 // throughput history for the Sparkline

	feed []ui.TimelineEvent // newest-last; trimmed to liveDashFeedCap
	jobs []liveDashJob
}

// liveDashState wraps the payload with the mutex the ticker holds while
// advancing the snapshot. The split keeps the lock out of every value
// copy (snapshot, render argument) — `go vet` would flag the alternative
// as "copies lock value".
type liveDashState struct {
	mu sync.Mutex
	d  liveDashData
}

// liveDash is the single-replica demo state. The ticker in setupServer
// advances it. Tests read from it directly; SSR + push both snapshot
// from it under the mutex.
var liveDash = func() *liveDashState {
	s := &liveDashState{d: liveDashData{
		throughput: 1840,
		workers:    12,
		depth:      46,
		p99:        142,
		ok:         1821,
		total:      1840,
		series:     []float64{1620, 1700, 1680, 1755, 1810, 1840},
		jobs: []liveDashJob{
			{ID: "job-ingest-04", Name: "ingest-batch-04", Status: "running", Progress: 62},
			{ID: "job-index-12", Name: "reindex-users", Status: "running", Progress: 28},
			{ID: "job-export-21", Name: "export-csv-daily", Status: "queued", Progress: 0},
		},
	}}
	s.d.feed = []ui.TimelineEvent{
		{Title: "ingest-batch-03 completed", Meta: "just now", Variant: ui.TimelineSuccess},
		{Title: "Queue depth above 100", Meta: "12s ago", Variant: ui.TimelineWarn},
		{Title: "Worker pool scaled to 12", Meta: "45s ago", Variant: ui.TimelineInfo},
	}
	return s
}()

// snapshot returns a deep-enough copy of the payload for SSR or push
// rendering. Callers must not hold the lock while rendering, so a copy
// keeps the per-tick render off the mutex.
func (s *liveDashState) snapshot() liveDashData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return liveDashData{
		throughput: s.d.throughput,
		workers:    s.d.workers,
		depth:      s.d.depth,
		p99:        s.d.p99,
		ok:         s.d.ok,
		total:      s.d.total,
		series:     append([]float64(nil), s.d.series...),
		feed:       append([]ui.TimelineEvent(nil), s.d.feed...),
		jobs:       append([]liveDashJob(nil), s.d.jobs...),
	}
}

// tick advances the demo state with a small random walk and occasional
// feed/job transitions. It is the synthesized source of "background
// events" — on a real app the equivalent writes come from your queue,
// metrics pipeline, or entity event bus.
func (s *liveDashState) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := &s.d

	// Metric random walk. Clamps keep the values in a realistic band.
	d.throughput = clampInt(d.throughput+int64(rand.IntN(400)-200), 500, 5000)
	d.workers = clampInt(d.workers+int64(rand.IntN(3)-1), 4, 24)
	d.depth = clampInt(d.depth+int64(rand.IntN(80)-40), 0, 800)
	d.p99 = clampInt(d.p99+int64(rand.IntN(40)-20), 50, 500)
	failed := int64(rand.IntN(6))
	d.total = d.throughput
	d.ok = d.total - failed

	// Throughput history for the Sparkline (rolling window).
	d.series = append(d.series, float64(d.throughput))
	if len(d.series) > liveDashSeriesCap {
		d.series = d.series[len(d.series)-liveDashSeriesCap:]
	}

	// Feed: add one event on most ticks. Trim server-side; the render
	// path also caps at liveDashFeedCap so the wire payload stays small.
	if rand.IntN(10) < 7 {
		d.feed = append(d.feed, dashRandomEvent(d.depth, d.p99))
		if len(d.feed) > liveDashFeedCap*2 {
			d.feed = d.feed[len(d.feed)-liveDashFeedCap*2:]
		}
	}

	// Jobs: advance running jobs; promote to done at 100.
	for i := range d.jobs {
		j := &d.jobs[i]
		if j.Status != "running" {
			continue
		}
		j.Progress += 8 + rand.IntN(18)
		if j.Progress >= 100 {
			j.Progress = 100
			j.Status = "done"
		}
	}
	// Occasionally rotate a finished job into a fresh queued one.
	if rand.IntN(4) == 0 {
		for i := range d.jobs {
			if d.jobs[i].Status == "done" {
				d.jobs[i] = dashRandomJob()
				break
			}
		}
	}
}

// dashIslandPusher is the seam the ticker uses to push island updates.
// Declared as an interface so the screen file need not import
// framework/uihost — setupServer wires the real *island.Manager in.
type dashIslandPusher interface {
	PresenceSessions(topic string) []string
	PushUpdate(island.IslandUpdate, string)
}

// pushAll re-renders every dashboard island from the current state and
// pushes each to every session on liveDashTopic. Called by the ticker
// after a tick. The push targets are PRESENCE SESSIONS — only sessions
// that joined the topic via ?presence= receive updates. This is the
// same wiring shape as the presence screen's OnPresenceChange. To isolate
// tenants, push to a per-tenant topic with per-tenant state — a fixed topic
// broadcasts identical HTML to every viewer (AuthorizeTopic gates the join,
// not the payload). See docs/live-dashboards "Tenant isolation".
func (s *liveDashState) pushAll(push dashIslandPusher) {
	snap := s.snapshot()
	islands := []struct {
		id string
		h  render.HTML
	}{
		{liveDashStatsID, renderDashStats(snap)},
		{liveDashFeedID, renderDashFeed(snap)},
		{liveDashJobsID, renderDashJobs(snap)},
	}
	for _, isl := range islands {
		update := island.IslandUpdate{IslandID: isl.id, HTML: string(isl.h)}
		for _, sid := range push.PresenceSessions(liveDashTopic) {
			push.PushUpdate(update, sid)
		}
	}
}

// dashStatusLabel derives the operational-status label from incident
// counts. It is mirrored by the JS reducer "dash.status" shipped via
// WithExtraScripts — the two MUST agree so the SSR-painted label matches
// the runtime-computed one (no flash on hydration).
func dashStatusLabel(open, ackd int) string {
	switch {
	case open <= 0:
		return "All systems operational"
	case open <= 2:
		return "Degraded — " + strconv.Itoa(open) + " open"
	default:
		return "Major incident — " + strconv.Itoa(open) + " open"
	}
}

// clampInt clamps v to the inclusive [lo, hi] band.
func clampInt(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// dashRandomEvent synthesizes one activity-feed entry derived from the
// current metrics — the kind of event an ops dashboard would surface.
func dashRandomEvent(depth, p99 int64) ui.TimelineEvent {
	switch rand.IntN(4) {
	case 0:
		return ui.TimelineEvent{Title: "Batch processed", Meta: "just now", Variant: ui.TimelineSuccess}
	case 1:
		if depth > 200 {
			return ui.TimelineEvent{Title: "Queue depth above 200", Meta: "just now", Variant: ui.TimelineWarn}
		}
		return ui.TimelineEvent{Title: "Throughput nominal", Meta: "just now", Variant: ui.TimelineInfo}
	case 2:
		if p99 > 300 {
			return ui.TimelineEvent{Title: "p99 latency above 300ms", Meta: "just now", Variant: ui.TimelineDanger}
		}
		return ui.TimelineEvent{Title: "Latency steady", Meta: "just now", Variant: ui.TimelineInfo}
	default:
		return ui.TimelineEvent{Title: "Worker pool rebalanced", Meta: "just now", Variant: ui.TimelineInfo}
	}
}

// dashJobCounter makes dashRandomJob produce stable, unique IDs.
var dashJobCounter uint64

// dashRandomJob synthesizes a fresh queued job.
func dashRandomJob() liveDashJob {
	n := atomic.AddUint64(&dashJobCounter, 1)
	names := []string{"ingest-batch", "reindex", "export-csv", "compact-segments", "snapshot-upload"}
	pick := names[int(n)%len(names)]
	suffix := strconv.FormatUint(n, 10)
	return liveDashJob{
		ID:       pick + "-" + suffix,
		Name:     pick + "-" + suffix,
		Status:   "queued",
		Progress: 0,
	}
}

// renderDashStats renders the metric StatCards island. Shared by SSR and
// the live push so the initial paint and every update produce identical
// markup. NO aria-live here — high-frequency numeric updates would flood
// assistive tech. The polite-announcement lane is the activity feed.
func renderDashStats(s liveDashData) render.HTML {
	direction := func(prev, cur int64) ui.TrendDirection {
		if cur > prev {
			return ui.TrendUp
		}
		if cur < prev {
			return ui.TrendDown
		}
		return ui.TrendFlat
	}
	cards := []render.HTML{
		ui.StatCard(ui.StatCardConfig{
			Label: "Throughput", Value: strconv.FormatInt(s.throughput, 10) + " ev/s",
			Direction: ui.TrendUp, Trend: "live",
		}),
		ui.StatCard(ui.StatCardConfig{
			Label: "Active workers", Value: strconv.FormatInt(s.workers, 10),
			Direction: ui.TrendFlat, Trend: "steady",
		}),
		ui.StatCard(ui.StatCardConfig{
			Label: "Queue depth", Value: strconv.FormatInt(s.depth, 10),
			Direction: direction(50, s.depth), Trend: "live",
		}),
		ui.StatCard(ui.StatCardConfig{
			Label: "Latency p99", Value: strconv.FormatInt(s.p99, 10) + " ms",
			Direction: direction(150, s.p99), Trend: "live",
		}),
	}
	return ui.Grid(ui.GridConfig{Min: "12rem"}, cards...)
}

// renderDashFeed renders the activity-feed Timeline. The server trims to
// liveDashFeedCap entries before rendering; the newest event is last.
// An empty feed renders a calm placeholder rather than panicking — a
// fresh boot with no events yet is a valid state.
func renderDashFeed(s liveDashData) render.HTML {
	events := s.feed
	if len(events) > liveDashFeedCap {
		events = events[len(events)-liveDashFeedCap:]
	}
	if len(events) == 0 {
		return html.Paragraph(html.TextConfig{Class: "ui-muted"},
			render.Text("No activity yet — events will appear here as they arrive."))
	}
	return ui.Timeline(ui.TimelineConfig{Events: events})
}

// renderDashJobs renders the jobs DataTable. Rows are keyed by job ID so
// successive pushes produce HTML that differs only on the rows that
// actually changed. Cells are pre-rendered HTML so formatting (status
// pills, progress text) is fully server-controlled.
func renderDashJobs(s liveDashData) render.HTML {
	rows := make([]ui.Row, len(s.jobs))
	for i, j := range s.jobs {
		progress := strconv.Itoa(j.Progress) + "%"
		if j.Status == "done" {
			progress = "100%"
		}
		rows[i] = ui.Row{
			ID: j.ID,
			Cells: map[string]render.HTML{
				"job":      render.Text(j.Name),
				"status":   render.Text(j.Status),
				"progress": render.Text(progress),
			},
		}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{
			{Key: "job", Header: "Job"},
			{Key: "status", Header: "Status"},
			{Key: "progress", Header: "Progress", Align: "end"},
		},
		Rows: rows,
	})
}

// renderDashConsole renders the operator-console region: the one place
// signals are used on this page. The +/- buttons mutate dash.incidentsOpen
// and dash.incidentsAckd client-side; the dashStatus computed slice
// derives the operational-status label and binds it to the StatusPill.
//
// dashStatus.Seed stamps the server-derived initial label so the SSR
// paint matches the runtime-computed one (no flash). The reducer in
// /__site/livedash-reducers.js must produce the same string for the
// same inputs — see dashStatusLabel above.
func renderDashConsole(ctx context.Context) render.HTML {
	open := dashIncidentsOpen.Default()
	ackd := dashIncidentsAckd.Default()
	// Seed the computed slice's initial SSR label so the first paint
	// matches what the runtime will recompute on hydration — no flash.
	// The reducer in /__site/livedash-reducers.js must produce the same
	// string for the same inputs (see dashStatusLabel above).
	dashStatus.Seed(ctx, dashStatusLabel(open, ackd))

	// +/- buttons. data-fui-signal-inc mutates the signal client-side
	// only — no RPC, no round-trip. The computed slice picks up the
	// change and re-derives the label. Negative deltas decrement.
	controls := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		ui.Button(ui.ButtonConfig{
			Label: "Open incident",
			ExtraAttrs: html.Attrs{
				"data-fui-signal-inc": dashIncidentsOpen.Name() + ":1",
				"aria-label":          "Open a new incident",
			},
		}),
		ui.Button(ui.ButtonConfig{
			Label:   "Resolve incident",
			Variant: ui.ButtonSecondary,
			ExtraAttrs: html.Attrs{
				"data-fui-signal-inc": dashIncidentsOpen.Name() + ":-1",
				"aria-label":          "Resolve one open incident",
			},
		}),
		ui.Button(ui.ButtonConfig{
			Label:   "Acknowledge",
			Variant: ui.ButtonSecondary,
			ExtraAttrs: html.Attrs{
				"data-fui-signal-inc": dashIncidentsAckd.Name() + ":1",
				"aria-label":          "Acknowledge one incident",
			},
		}),
	)

	// Bound StatusPill. dashStatus.Bind emits:
	//   <span data-fui-signal="dash.status"
	//         data-fui-computed="dash.status"
	//         data-fui-computed-deps="dash.incidentsOpen,dash.incidentsAckd">
	//     {seeded initial label}
	//   </span>
	// The computed module subscribes to the deps, runs the reducer on
	// change, and fans the result through the signal to this same span.
	statusPill := dashStatus.Bind(ctx, "span", map[string]string{
		"class":         "ui-status-pill ui-status-pill--accent",
		"data-fui-comp": "ui-status-pill",
		"aria-live":     "polite",
		"aria-atomic":   "true",
	})

	return ui.Card(ui.CardConfig{
		Heading:      "Operational status",
		HeadingLevel: 2,
		Description:  "A store.Computed slice derives this label from two client-side signals. The +/- buttons mutate them locally; the reducer runs in the browser and the pill updates live — no RPC, no round-trip.",
	},
		controls,
		html.Div(html.DivConfig{Role: "status", AriaLabel: "Operational status"},
			statusPill,
			html.Paragraph(html.TextConfig{Class: "ui-muted"},
				render.Text("Open "+strconv.Itoa(open)+" · Acknowledged "),
				// Live-bound count: dashIncidentsAckd.Bind emits a
				// <span data-fui-signal="dash.incidentsAckd"> that the
				// runtime updates in place whenever the Acknowledge
				// button's data-fui-signal-inc fires. Without this
				// bind the click increments the signal but the visible
				// count stays at the SSR-painted 0 forever.
				dashIncidentsAckd.Bind(ctx, "span", map[string]string{
					"class": "ui-muted",
				}),
			),
		),
	)
}

// LiveDashboardScreen is the /examples/live-dashboard page.
type LiveDashboardScreen struct {
	component.ContextOnly
}

func (s *LiveDashboardScreen) ScreenTitle() string { return "Live dashboard" }
func (s *LiveDashboardScreen) ScreenDescription() string {
	return "Live-data reference: SSE island push, computed signals, bounded feed"
}
func (s *LiveDashboardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LiveDashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	snap := liveDash.snapshot()

	return html.Main(html.MainConfig{Class: "livedash-page"},
		// Connection-health banner. Sits above the content. SSESilenceMs
		// trips it if the ticker goes quiet (the runtime polls
		// window.__gofastr.sseStatus.lastEventAt). The Retry button
		// probes /__site/livedash/health, which returns 204 when the
		// server is up.
		ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{
			HealthEndpoint: "/__site/livedash/health",
			SSESilenceMs:   6000,
			Title:          "Live updates paused",
			Description:    "The dashboard's SSE stream went quiet. Your last-known values are still on screen; reconnect to refresh.",
			RetryLabel:     "Reconnect",
		}),
		container(
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "Example · Live dashboards",
				Title:    "Queue ops — live dashboard",
				Subtitle: "A realistic ops dashboard composed from existing primitives: SSE island push for metric StatCards, a bounded Timeline activity feed, a keyed jobs DataTable, a store.Computed status label, and the connection-health banner. See /docs/live-dashboards for the composition guide.",
			}),

			// Metric StatCards island. The ticker pushes fresh HTML for
			// this slot every tick; the runtime swaps its innerHTML.
			// NOT an aria-live region — high-frequency numeric updates
			// must not flood assistive tech. The polite lane is the
			// activity feed below.
			html.Div(html.DivConfig{
				Class:      "livedash-stats-slot",
				ExtraAttrs: html.Attrs{"data-island": liveDashStatsID},
				AriaLabel:  "Live metrics",
			}, renderDashStats(snap)),

			// Two-column grid: activity feed (left) + jobs (right).
			// Each region is a wrapper that holds an SSR-only heading
			// sibling ABOVE the data-island slot. The island wraps ONLY
			// the replaceable content (Timeline/DataTable); on push the
			// runtime does island.innerHTML = payload, so anything that
			// must survive a tick (the heading) lives OUTSIDE the slot.
			ui.Grid(ui.GridConfig{Min: "24rem", Gap: ui.GapLG},
				// Activity feed — the polite announcement lane. role=status
				// implies aria-live=polite; the data-island slot is the
				// stable parent so changes inside it get announced without
				// the slot itself flickering.
				html.Div(html.DivConfig{},
					html.Heading(html.HeadingConfig{Level: 2, Class: "livedash-region-title"},
						render.Text("Activity feed")),
					html.Div(html.DivConfig{
						Class:      "livedash-feed-slot",
						ExtraAttrs: html.Attrs{"data-island": liveDashFeedID},
						Role:       "status",
						AriaLabel:  "Recent activity",
					}, renderDashFeed(snap)),
				),

				// Jobs table. The heading is a sibling of the data-island
				// slot so the push only swaps the table, not the title.
				// Row.ID keys rows so successive pushes differ only on
				// changed rows.
				html.Div(html.DivConfig{},
					html.Heading(html.HeadingConfig{Level: 2, Class: "livedash-region-title"},
						render.Text("Jobs")),
					html.Div(html.DivConfig{
						Class:      "livedash-jobs-slot",
						ExtraAttrs: html.Attrs{"data-island": liveDashJobsID},
						AriaLabel:  "Job status",
					}, renderDashJobs(snap)),
				),
			),

			// Operator console — the one signal-bound region on the page.
			renderDashConsole(ctx),

			// How-it-works notes (no live data; static prose).
			html.Div(html.DivConfig{Class: "livedash-notes"},
				html.Heading(html.HeadingConfig{Level: 2}, render.Text("How this is wired")),
				html.UnorderedList(html.ListConfig{},
					html.ListItem(html.ListItemConfig{},
						render.Text("Open this page in a second browser (or private window) — both see the same metric ticks because the push is broadcast to every session on the "+liveDashTopic+" presence topic.")),
					html.ListItem(html.ListItemConfig{},
						render.Text("SSE is best-effort: a dropped frame is gone. The dashboard reconstructs on reconnect — point a fetch at /__site/livedash/refresh?island=stats to reconcile.")),
					html.ListItem(html.ListItemConfig{},
						render.Text("On an authenticated app you would set host.Islands.AuthorizeTopic to gate "+liveDashTopic+" by tenant — the push wiring is identical, only the ACL changes.")),
					html.ListItem(html.ListItemConfig{},
						render.Text("Metric StatCards are NOT aria-live (high-frequency numbers would flood screen readers); the activity feed carries the polite-announcement lane.")),
				),
			),
		),
	)
}
