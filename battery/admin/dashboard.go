package admin

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
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

// dashboardNew is the header's New button for Config.DashboardNew.
func (b *Battery) dashboardNew(ctx context.Context) render.HTML {
	e, ok := b.exposedNamed(b.cfg.DashboardNew)
	if b.cfg.DashboardNew == "" || !ok {
		return ""
	}
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:   i18nui.EntityNew.Text(ctx, map[string]any{"entity": b.singular(ctx, e)}),
		Href:    b.entityBase(e) + "/create",
		Variant: ui.ButtonPrimary,
		Icon:    "plus",
	})
}

// renderDashboard draws the dashboard.
func (b *Battery) renderDashboard(ctx context.Context, _ map[string]string) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.AdminDashboard.Text(ctx),
			Subtitle: i18nui.AdminDashboardSub.Text(ctx),
			Actions:  b.dashboardNew(ctx),
		}),
		resultNotice(ctx),
	}
	if strip := b.metricStrip(ctx); strip != "" {
		parts = append(parts, strip)
	}
	parts = append(parts, b.entityCards(ctx)...)
	var ops []render.HTML
	if b.db != nil {
		ops = append(ops, b.recentCard(ctx))
	}
	if len(b.cfg.Attention) > 0 || (b.cfg.Queue != nil && platformCaller(ctx)) {
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
		label := i18nui.AdminEntities.Text(ctx)
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
// one the count spent. It reads the number when there are fewer,
// "10,000+" past it, and "—" when the read fails too.
func (b *Battery) probeCount(ctx context.Context, e *entity.Entity) string {
	pctx, cancel := context.WithTimeout(ctx, probeDeadline)
	defer cancel()
	n, more, ok := b.ui.CountUpTo(pctx, e.GetName(), "", manyRows)
	if more {
		n = manyRows
	}
	return countText(ctx, n, ok)
}

// entityStat is the card itself: the plural, the count read in the
// caller's scope, when the newest of those records was written, a link
// to the list and, when the caller may create one, New.
func (b *Battery) entityStat(ctx context.Context, e *entity.Entity) render.HTML {
	cctx, cancel := context.WithTimeout(ctx, countDeadline)
	defer cancel()
	n, ok := b.ui.CountInt(cctx, e.GetName(), "")
	count := countText(ctx, n, ok)
	if count == "—" {
		count = b.probeCount(ctx, e)
	}
	updated := ""
	if t, ok := b.ui.LastUpdated(cctx, e.GetName()); ok {
		updated = i18nui.AdminUpdatedAgo.Text(ctx, map[string]any{"ago": ui.Ago(ctx, time.Now(), t)})
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
		Tile:  true,
		Action: ui.LinkButton(ui.LinkButtonConfig{
			Label:   i18nui.AdminCardNew.Text(ctx),
			Href:    b.entityBase(e) + "/create",
			Variant: ui.ButtonSecondary,
			Size:    ui.ButtonSizeSmall,
			Icon:    "plus",
		}),
	})
}

// failedJobs is Needs attention's failed jobs: the count, a link to
// the failed filter and the newest rows, each with Replay. Nothing
// failed draws nothing; a list the queue could not give says so.
func (b *Battery) failedJobs(ctx context.Context) render.HTML {
	jobs, err := b.cfg.Queue.ListJobs(ctx, "failed", dashboardRows, 0)
	if err != nil {
		b.logger().Error("admin: list failed jobs", "error", err)
		return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.AdminQueueLoadFailed.Text(ctx)))
	}
	if len(jobs) == 0 {
		return ""
	}
	var count render.HTML
	if stats, err := b.cfg.Queue.Stats(ctx); err == nil {
		count = ui.Muted(render.Text(i18n.Number(ctx, stats["failed"])))
	}
	_, replay := b.replayable()
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{
			Title:        i18nui.AdminFailedJobs.Text(ctx),
			Badge:        count,
			Actions:      headerLink(i18nui.AdminViewAll.Text(ctx), b.cfg.PathPrefix+"/queue?status=failed"),
			HeadingLevel: 3,
			Compact:      true,
		}),
		b.jobsTable(ctx, jobs, "failed", b.cfg.PathPrefix, replay, 3, nil, true),
	)
}

// recentCard is the newest audit rows, scoped as the Audit log page is.
func (b *Battery) recentCard(ctx context.Context) render.HTML {
	title := i18nui.AdminRecent.Text(ctx)
	all := headerLink(i18nui.AdminAudit.Text(ctx), b.cfg.PathPrefix+"/audit")
	rows, err := b.queryAudit(ctx, dashboardRows)
	if err != nil {
		b.logger().Error("admin: load audit rows", "table", b.cfg.AuditTable, "error", err)
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.AdminAuditLoadFailed.Text(ctx))))
	}
	if len(rows) == 0 {
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.AdminNoActivity.Text(ctx), HeadingLevel: 3}))
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
		cards[i] = ui.Card(ui.CardConfig{Heading: c.Title.Text(ctx), HeadingLevel: 2}, body)
	}
	return ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG}, cards...)
}

// mountOps mounts the ops posts, each only when its page is drawn, and
// the fragments the dashboard polls.
func (b *Battery) mountOps(r *router.Router) {
	p := b.cfg.PathPrefix
	post := func(path string, h http.HandlerFunc) { r.Post(p+path, h) }
	if b.cfg.Queue != nil {
		post("/queue/_replay/{id}", platformOnly(b.handleReplay))
		post("/queue/_replay_all", platformOnly(b.handleReplayAll))
	}
	if b.cfg.Policy != nil && b.cfg.GrantStore != nil {
		post("/rbac/_grant", platformOnly(b.handleGrant))
		post("/rbac/_permissions", platformOnly(b.handlePermissions))
	}
	if b.cfg.Auth != nil {
		post("/rbac/_assign", platformOnly(b.handleAssign))
		post("/account/_name", b.handleName)
	}
	if b.cfg.ProcessModules != nil {
		post("/modules/_enable", platformOnly(b.handleModuleEnable))
		post("/modules/_disable", platformOnly(b.handleModuleDisable))
		post("/modules/_bump", platformOnly(b.handleModuleBump))
		post("/modules/_revoke", platformOnly(b.handleModuleRevoke))
	}
	b.mountCards(r)
	b.mountMetrics(r)
}

// pathSegment escapes one path segment.
func pathSegment(s string) string { return url.PathEscape(s) }

// urlQueryEscape escapes a query value.
func urlQueryEscape(s string) string { return url.QueryEscape(s) }
