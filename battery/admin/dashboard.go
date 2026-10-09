package admin

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The dashboard: a card per exposed entity under its nav group, each
// polling its own count; the failed jobs; the recent activity; then the
// app's own cards. Everything refreshes by polling, never by a push.

// dashboardRows is how many failed jobs and audit rows the dashboard
// shows.
const dashboardRows = 5

// countDeadline bounds one card's count, so a slow table cannot hold the
// dashboard; a count past it reads "—" until the next poll.
var countDeadline = 2 * time.Second

// probeDeadline bounds the read a card falls back to when its count is
// late: at most manyRows ids, which a slow COUNT(*) on a large table
// usually is not.
const probeDeadline = time.Second

// renderDashboard draws the dashboard.
func (b *Battery) renderDashboard(ctx context.Context, _ map[string]string) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminDashboard),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminDashboardSub),
		}),
		resultNotice(ctx),
	}
	if strip := b.metricStrip(ctx); strip != "" {
		parts = append(parts, strip)
	}
	parts = append(parts, b.entityCards(ctx)...)
	var ops []render.HTML
	if b.cfg.Queue != nil {
		ops = append(ops, b.failedJobsCard(ctx))
	}
	if b.db != nil {
		ops = append(ops, b.recentCard(ctx))
	}
	if len(b.cfg.Attention) > 0 {
		ops = append(ops, b.attentionCard(ctx))
	}
	if len(ops) > 0 {
		parts = append(parts, ui.Grid(ui.GridConfig{Min: "24rem", Gap: ui.GapLG}, ops...))
	}
	if cards := b.appCards(ctx); cards != "" {
		parts = append(parts, cards)
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapXL}, parts...)
}

// entityCards are the entity cards, a section per nav group in the
// sidebar's order.
func (b *Battery) entityCards(ctx context.Context) []render.HTML {
	var order []string
	byGroup := map[string][]render.HTML{}
	for _, e := range b.ents {
		n := navOf(e)
		if n != nil && n.Hide {
			continue
		}
		g := ""
		if n != nil {
			g = n.Group
		}
		if _, seen := byGroup[g]; !seen {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], b.entityCard(ctx, e))
	}
	out := make([]render.HTML, 0, len(order))
	for _, g := range order {
		label := i18nui.T(ctx, i18nui.KeyAdminEntities)
		if g != "" {
			label = i18nui.NavGroupLabel(ctx, nil, g, "")
		}
		out = append(out, ui.Section(ui.SectionConfig{Heading: label, Compact: true, Overline: true},
			ui.Grid(ui.GridConfig{Min: "14rem", Fill: true}, byGroup[g]...)))
	}
	return out
}

// entityCard is one entity's card inside the element that polls it:
// the poll answer replaces the card with a fresh one.
func (b *Battery) entityCard(ctx context.Context, e *entity.Entity) render.HTML {
	return html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
		"data-cui-poll":     countPoll,
		"data-cui-poll-src": b.cfg.PathPrefix + "/_count/" + url.PathEscape(e.GetName()),
	}}, b.entityStat(ctx, e))
}

// probeCount is a late count's fallback: a bounded read of at most
// manyRows ids in the caller's scope, its own deadline apart from the
// one the count spent. It reads the number when there are fewer, "10k+"
// past it, and "—" when the read fails too.
func (b *Battery) probeCount(ctx context.Context, e *entity.Entity) string {
	pctx, cancel := context.WithTimeout(ctx, probeDeadline)
	defer cancel()
	n, more, ok := b.ui.CountUpTo(pctx, e.GetName(), "", manyRows)
	switch {
	case !ok:
		return "—"
	case more:
		return countText(ctx, strconv.Itoa(manyRows))
	}
	return countText(ctx, strconv.Itoa(n))
}

// entityStat is the card itself: the plural, the count read in the
// caller's scope, when the newest of those records was written, a link
// to the list and, when the caller may create one, New.
func (b *Battery) entityStat(ctx context.Context, e *entity.Entity) render.HTML {
	cctx, cancel := context.WithTimeout(ctx, countDeadline)
	defer cancel()
	count := countText(ctx, b.ui.StatValue(cctx, e.GetName(), "count", "", "", ""))
	if count == "—" {
		count = b.probeCount(ctx, e)
	}
	updated := ""
	if t, ok := b.ui.LastUpdated(cctx, e.GetName()); ok {
		updated = i18nui.TVars(ctx, i18nui.KeyAdminUpdatedAgo, map[string]string{"ago": ui.Ago(ctx, time.Now(), t)})
	}
	icon := ""
	if n := navOf(e); n != nil {
		icon = n.Icon
	}
	return ui.StatCard(ui.StatCardConfig{
		Label: b.plural(ctx, e),
		Value: count,
		Trend: updated,
		Href:  b.entityBase(e),
		Icon:  icon,
		Action: ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.T(ctx, i18nui.KeyAdminCardNew),
			Href:    b.entityBase(e) + "/create",
			Variant: ui.ButtonSecondary,
			Size:    ui.ButtonSizeSmall,
			Icon:    "plus",
		}),
	})
}

// failedJobsCard is the failed jobs count and the newest failures, each
// with Replay.
func (b *Battery) failedJobsCard(ctx context.Context) render.HTML {
	title := i18nui.T(ctx, i18nui.KeyAdminFailedJobs)
	all := headerLink(i18nui.T(ctx, i18nui.KeyAdminQueue), b.cfg.PathPrefix+"/queue?status=failed")
	jobs, err := b.cfg.Queue.ListJobs(ctx, "failed", dashboardRows, 0)
	if err != nil {
		b.logger().Error("admin: list failed jobs", "error", err)
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminQueueLoadFailed))))
	}
	count := ""
	if stats, err := b.cfg.Queue.Stats(ctx); err == nil {
		count = strconv.Itoa(stats["failed"])
	}
	_, replay := b.replayable()
	body := ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminNoFailedJobs), HeadingLevel: 3})
	if len(jobs) > 0 {
		body = b.jobsTable(ctx, jobs, "failed", b.cfg.PathPrefix, replay, 3, nil)
	}
	cfg := ui.CardConfig{Heading: title, HeadingLevel: 2, Action: all}
	if count != "" {
		cfg.Description = count
	}
	return ui.Card(cfg, body)
}

// recentCard is the newest audit rows, scoped as the Audit log page is.
func (b *Battery) recentCard(ctx context.Context) render.HTML {
	title := i18nui.T(ctx, i18nui.KeyAdminRecent)
	all := headerLink(i18nui.T(ctx, i18nui.KeyAdminAudit), b.cfg.PathPrefix+"/audit")
	rows, err := b.queryAudit(ctx, dashboardRows)
	if err != nil {
		b.logger().Error("admin: load audit rows", "table", b.cfg.AuditTable, "error", err)
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminAuditLoadFailed))))
	}
	if len(rows) == 0 {
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminNoActivity), HeadingLevel: 3}))
	}
	names := b.actorNames(ctx, rows)
	now := time.Now()
	events := make([]ui.TimelineEvent, len(rows))
	for i, r := range rows {
		before, after := auditSides(r)
		events[i] = ui.TimelineEvent{
			Lead:    b.activityLead(ctx, names, r, before, after),
			Body:    b.activityChanges(ctx, r, before, after),
			Meta:    ui.Ago(ctx, now, r.CreatedAt),
			Variant: timelineVariant(r.Op),
			Icon:    opIcon(r.Op),
		}
	}
	return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2, Action: all}, ui.Timeline(ui.TimelineConfig{Events: events}))
}

// headerLink is a dashboard card's way to its full page, at the end of
// the card's header.
func headerLink(label, href string) render.HTML {
	return ui.LinkButton(ui.LinkButtonConfig{Label: label, Href: href, Variant: ui.ButtonGhost, Size: ui.ButtonSizeSmall})
}

// opIcon is the icon an audit operation's activity marker draws.
func opIcon(op string) string {
	switch opKind(op) {
	case "create":
		return "plus"
	case "update":
		return "pencil"
	case "delete", "purge":
		return "trash"
	case "restore":
		return "rotate-ccw"
	case "transition", "override", "replay":
		return "repeat"
	case "grant", "revoke":
		return "shield"
	}
	return "activity"
}

// timelineVariant tints an audit operation's dot.
func timelineVariant(op string) ui.TimelineEventVariant {
	switch opVariant(op) {
	case ui.StatusSuccess:
		return ui.TimelineSuccess
	case ui.StatusDanger:
		return ui.TimelineDanger
	case ui.StatusInfo:
		return ui.TimelineInfo
	}
	return ui.TimelineNeutral
}

// appCards are the app's own dashboard cards, each in a Card; a polled
// one sits in the element that re-fetches its body.
func (b *Battery) appCards(ctx context.Context) render.HTML {
	if len(b.cfg.Cards) == 0 {
		return ""
	}
	cards := make([]render.HTML, len(b.cfg.Cards))
	for i, c := range b.cfg.Cards {
		body := b.buildSlot(ctx, "card "+c.Key, c.Build)
		if c.Poll > 0 {
			body = html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
				"data-cui-poll":     c.Poll.String(),
				"data-cui-poll-src": b.cardPath(c.Key),
			}}, body)
		}
		cards[i] = ui.Card(ui.CardConfig{Heading: c.Title, HeadingLevel: 2}, body)
	}
	return ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG}, cards...)
}

// mountOps mounts the ops posts, each only when its page is drawn, and
// the fragments the dashboard polls.
func (b *Battery) mountOps(r *router.Router) {
	p := b.cfg.PathPrefix
	post := func(path string, h http.HandlerFunc) { r.Post(p+path, h) }
	if b.cfg.Queue != nil {
		post("/queue/_replay/{id}", b.handleReplay)
		post("/queue/_replay_all", b.handleReplayAll)
	}
	if b.cfg.Policy != nil && b.cfg.GrantStore != nil {
		post("/rbac/_grant", b.handleGrant)
		post("/rbac/_permissions", b.handlePermissions)
	}
	if b.cfg.Auth != nil {
		post("/rbac/_assign", b.handleAssign)
	}
	if b.cfg.ProcessModules != nil {
		post("/modules/_enable", b.handleModuleEnable)
		post("/modules/_disable", b.handleModuleDisable)
		post("/modules/_bump", b.handleModuleBump)
		post("/modules/_revoke", b.handleModuleRevoke)
	}
	b.mountCards(r)
	b.mountMetrics(r)
}

// pathSegment escapes one path segment.
func pathSegment(s string) string { return url.PathEscape(s) }

// urlQueryEscape escapes a query value.
func urlQueryEscape(s string) string { return url.QueryEscape(s) }
