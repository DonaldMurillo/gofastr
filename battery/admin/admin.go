// Package admin is a small read-only admin battery for GoFastr apps —
// stock screens on top of the data the framework already collects:
// queue jobs (when [battery/queue] is wired) and the audit log (when
// [framework.WithAuditLog] is set).
//
// The battery mounts three pages:
//
//	GET /admin           index with per-section summary cards
//	GET /admin/queue     jobs list with ?status= filter
//	GET /admin/audit     audit log paged newest-first
//
// Pages are self-contained server-rendered HTML — they don't depend
// on [framework/uihost] or the runtime, so the admin endpoints work
// even before any UI host is mounted. Wire your own auth middleware
// in front of them; nothing here gates access.
package admin

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Config configures the Admin battery.
type Config struct {
	// PathPrefix is the URL prefix under which admin pages mount.
	// Defaults to "/admin".
	PathPrefix string

	// Title is the title shown at the top of every admin page.
	// Defaults to "Admin".
	Title string

	// Queue is the optional Browsable queue. When set, /admin/queue is
	// active. When nil, that page returns a "no queue wired" stub so
	// the route never 404s ambiguously.
	Queue queue.Browsable

	// DB is the database connection used to read the audit log table.
	// When nil, /admin/audit returns a "no audit log wired" stub.
	DB *sql.DB

	// AuditTable is the audit log table name. Defaults to "audit_log".
	AuditTable string

	// QueueListLimit caps rows on /admin/queue. Default 200.
	QueueListLimit int

	// AuditListLimit caps rows on /admin/audit. Default 200.
	AuditListLimit int

	// Entities lists the entity names to expose as editable CRUD screens
	// under <PathPrefix>/e/<table>. When empty (default), the battery exposes
	// EVERY registered entity whose CRUD is enabled — the "generate the whole
	// back-office" default. CRUD-disabled entities (e.g. battery/auth's
	// users/sessions, which ship CRUD=false) are skipped automatically, so the
	// default never exposes credential tables. Name an entity explicitly to
	// override (including a CRUD=false one, if you really mean to). Screens
	// proxy to each entity's own CrudHandler, so validation, owner/tenant
	// scope, hooks, and events all apply exactly as on the JSON API.
	Entities []string

	// Authorize gates every admin surface — both the SSR screens (via the UI
	// host's policy chain) and the RPC/form routes (via middleware). It returns
	// true to allow the request. When nil, the default authorizer requires an
	// authenticated user that holds the AdminRole (see below) — a user whose
	// GetRoles() []string includes it. Supply a custom predicate to override
	// the role check entirely (e.g. a permission lookup, an allow-list).
	Authorize func(ctx context.Context) bool

	// AdminRole is the role the default authorizer requires (when Authorize is
	// nil). Defaults to "admin". Ignored when Authorize is set.
	AdminRole string

	// EntityListLimit caps rows per page on an entity list screen. Default 50.
	EntityListLimit int
}

// Battery is the framework Battery implementation.
type Battery struct {
	cfg      Config
	registry *framework.Registry // set at Init; enables the entity CRUD screens
	db       *sql.DB             // effective DB for entity CRUD (cfg.DB or app.DB)
	host     *uihost.UIHost      // the app's mounted UI host (entity screens render through it)
	screens  *appui.App          // host.App — where entity CRUD screens register
	router   *router.Router      // the framework router (entity RPC/form/delete routes)
	flash    *flashStore         // short-lived form re-render payloads (validation errors + values)
}

// New constructs the Admin battery with the supplied config. Pass the
// result to framework.App.RegisterBattery.
func New(cfg Config) *Battery {
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "/admin"
	}
	cfg.PathPrefix = strings.TrimRight(cfg.PathPrefix, "/")
	if cfg.Title == "" {
		cfg.Title = "Admin"
	}
	if cfg.AuditTable == "" {
		cfg.AuditTable = "audit_log"
	}
	if cfg.QueueListLimit <= 0 {
		cfg.QueueListLimit = 200
	}
	if cfg.AuditListLimit <= 0 {
		cfg.AuditListLimit = 200
	}
	if cfg.EntityListLimit <= 0 {
		cfg.EntityListLimit = 50
	}
	return &Battery{cfg: cfg, db: cfg.DB, flash: newFlashStore()}
}

// authorized reports whether the current request may use the admin. The
// default (no Authorize configured) requires an authenticated user that holds
// the configured AdminRole — secure by default. A custom Authorize overrides
// the role check entirely.
func (b *Battery) authorized(ctx context.Context) bool {
	if b.cfg.Authorize != nil {
		return b.cfg.Authorize(ctx)
	}
	// Require a NON-NIL user. battery/auth's SessionMiddleware seeds a nil user
	// on every request (so GetCurrentUser works) and only fills it in when
	// authenticated — so `ok` alone is true even for anonymous callers. The
	// nil check is what actually gates anonymous callers out.
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return false
	}
	// Secure by default: an authenticated user is NOT automatically an admin.
	// Require the AdminRole via the structural GetRoles interface (battery/auth's
	// User satisfies it). A user that can't prove a role is denied — set a custom
	// Config.Authorize to use a different model.
	rh, ok := u.(interface{ GetRoles() []string })
	if !ok {
		return false
	}
	want := b.adminRole()
	for _, role := range rh.GetRoles() {
		if role == want {
			return true
		}
	}
	return false
}

// adminRole returns the configured admin role, defaulting to "admin".
func (b *Battery) adminRole() string {
	if b.cfg.AdminRole != "" {
		return b.cfg.AdminRole
	}
	return "admin"
}

// authzStatus maps a failed authorization to an HTTP status: 401 when no user
// is present (authenticate first), 403 when a user is present but lacks admin
// rights (authenticated, just not allowed).
func (b *Battery) authzStatus(ctx context.Context) int {
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "admin" }

// Init implements framework.Battery. Mounts the three admin pages on
// the App's router under cfg.PathPrefix.
func (b *Battery) Init(app *framework.App) error {
	b.registry = app.Registry
	if b.db == nil {
		b.db = app.DB
	}
	// Discover the app's mounted UI host so entity CRUD screens render through
	// its pipeline (runtime.js hydration, islands, widgets) instead of a second
	// host. Batteries Init at App.Start, after Mount, so the host is present by
	// now if one was mounted.
	for _, m := range app.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			b.host = h
			b.screens = h.App
			break
		}
	}
	b.router = app.Router()
	b.RegisterRoutes(app.Router())
	return b.registerEntityAdmin()
}

// RegisterRoutes mounts the three admin pages under cfg.PathPrefix on
// the supplied router. Exposed so apps that compose their own router
// can mount the admin without going through the battery lifecycle.
func (b *Battery) RegisterRoutes(r *router.Router) {
	hdr := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})
	guard := func(h http.HandlerFunc) http.Handler { return hdr(b.gate(h)) }

	// Stylesheet served from a same-origin route rather than an inline <style>
	// block — the battery's strict CSP (default-src 'self', no 'unsafe-inline')
	// would otherwise block inline styles in the browser, rendering the admin
	// unstyled. Ungated: it carries no data and lets the 401 page degrade
	// gracefully. SecurityHeaders still applies.
	r.Get(b.cfg.PathPrefix+"/admin.css", hdr(http.HandlerFunc(b.handleCSS)))

	r.Get(b.cfg.PathPrefix, guard(b.handleIndex))
	r.Get(b.cfg.PathPrefix+"/queue", guard(b.handleQueue))
	r.Get(b.cfg.PathPrefix+"/audit", guard(b.handleAudit))
}

// gate wraps a route handler so it refuses unauthorized callers (401). The
// framework auth chain sets the user; b.authorized decides. Used for the
// standalone ops pages and the entity RPC/form routes.
func (b *Battery) gate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.authorized(r.Context()) {
			status := b.authzStatus(r.Context())
			http.Error(w, http.StatusText(status), status)
			return
		}
		next(w, r)
	})
}

// ----- handlers ------------------------------------------------------------

func (b *Battery) handleIndex(w http.ResponseWriter, r *http.Request) {
	var stats queue.JobStats
	if b.cfg.Queue != nil {
		stats, _ = b.cfg.Queue.Stats(r.Context())
	}
	var auditCount int
	if b.cfg.DB != nil {
		_ = b.cfg.DB.QueryRowContext(r.Context(),
			fmt.Sprintf("SELECT COUNT(*) FROM %s", b.cfg.AuditTable),
		).Scan(&auditCount)
	}
	body := render.Raw("") +
		section("Queue", queueSummary(b.cfg.PathPrefix, stats, b.cfg.Queue != nil)) +
		section("Audit log", auditSummary(b.cfg.PathPrefix, auditCount, b.cfg.DB != nil))
	b.writePage(w, b.cfg.Title, "Overview", body)
}

func (b *Battery) handleQueue(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Queue == nil {
		b.writePage(w, b.cfg.Title, "Queue",
			render.Raw(`<p class="muted">No queue is wired into this admin battery.</p>`))
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.QueueListLimit)
	jobs, err := b.cfg.Queue.ListJobs(r.Context(), status, limit)
	if err != nil {
		// Don't echo err.Error() — driver text leaks DSNs, IPs, secrets.
		b.writePage(w, b.cfg.Title, "Queue",
			render.Raw(`<p class="err">Could not load queue jobs. Check the server logs for details.</p>`))
		return
	}
	stats, _ := b.cfg.Queue.Stats(r.Context())
	body := queueFilters(b.cfg.PathPrefix, status, stats) +
		jobsTable(jobs)
	b.writePage(w, b.cfg.Title, "Queue", body)
}

func (b *Battery) handleAudit(w http.ResponseWriter, r *http.Request) {
	if b.cfg.DB == nil {
		b.writePage(w, b.cfg.Title, "Audit log",
			render.Raw(`<p class="muted">No DB / audit table is wired into this admin battery.</p>`))
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.AuditListLimit)
	rows, err := b.queryAudit(r.Context(), limit)
	if err != nil {
		// Don't echo err.Error() — driver text leaks DSNs, schema, secrets.
		b.writePage(w, b.cfg.Title, "Audit log",
			render.Raw(`<p class="err">Could not load audit rows. Check the server logs for details.</p>`))
		return
	}
	b.writePage(w, b.cfg.Title, "Audit log", auditTable(rows))
}

// ----- audit query ---------------------------------------------------------

// auditRow is the local DTO used by the audit page; the framework
// audit table can carry any subset of (actor_id, diff) so we treat
// them as nullable here rather than the framework's audit struct.
type auditRow struct {
	ID        string
	Entity    string
	Op        string
	RecordID  string
	ActorID   sql.NullString
	CreatedAt time.Time
	Diff      sql.NullString
}

func (b *Battery) queryAudit(ctx context.Context, limit int) ([]auditRow, error) {
	q := fmt.Sprintf(`SELECT id, entity, op, record_id, actor_id, created_at, diff
		FROM %s ORDER BY created_at DESC LIMIT %d`, b.cfg.AuditTable, limit)
	rows, err := b.cfg.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.ID, &r.Entity, &r.Op, &r.RecordID, &r.ActorID, &r.CreatedAt, &r.Diff); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ----- rendering helpers ---------------------------------------------------

const baseCSS = `
:root { color-scheme: light dark; }
body { font-family: -apple-system, system-ui, sans-serif; margin: 0; padding: 2rem;
       max-width: 80rem; margin-inline: auto; line-height: 1.5; }
h1 { margin: 0 0 0.25rem; font-size: 1.5rem; }
.sub { color: #6b7280; margin: 0 0 1.5rem; }
nav { display: flex; gap: 1rem; padding-block: 1rem; border-bottom: 1px solid #d1d5db;
      margin-bottom: 1.5rem; }
nav a { color: inherit; text-decoration: none; padding: 0.25rem 0.5rem; border-radius: 4px; }
nav a:hover { background: rgba(0,0,0,0.05); }
section { margin-bottom: 2rem; }
section h2 { font-size: 1.1rem; margin: 0 0 0.5rem; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
         gap: 0.75rem; }
.card { padding: 0.75rem 1rem; border: 1px solid #d1d5db; border-radius: 6px; }
.card .label { font-size: 0.8rem; color: #6b7280; text-transform: uppercase; letter-spacing: 0.05em; }
.card .value { font-size: 1.5rem; font-weight: 600; }
.muted { color: #6b7280; }
.err { color: #b91c1c; }
table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
th, td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid #e5e7eb; }
th { background: rgba(0,0,0,0.03); font-weight: 600; }
tr:hover td { background: rgba(0,0,0,0.02); }
.filters { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-bottom: 1rem; }
.filters a { padding: 0.25rem 0.75rem; border-radius: 999px; border: 1px solid #d1d5db;
             text-decoration: none; color: inherit; font-size: 0.85rem; }
.filters a.active { background: #111827; color: white; border-color: #111827; }
code { font-family: ui-monospace, SFMono-Regular, monospace; font-size: 0.85em; }
nav a.current { background: rgba(0,0,0,0.08); font-weight: 600; }
.toolbar { display: flex; align-items: center; gap: 1rem; margin-bottom: 1rem; }
.toolbar .muted { margin-left: auto; font-size: 0.85rem; }
.btn { display: inline-block; padding: 0.4rem 0.9rem; border: 1px solid #d1d5db; border-radius: 6px;
       text-decoration: none; color: inherit; font-size: 0.9rem; background: none; cursor: pointer; }
.btn:hover { background: rgba(0,0,0,0.04); }
.btn.primary { background: #111827; color: white; border-color: #111827; }
.btn.primary:hover { background: #1f2937; }
.pager { display: flex; gap: 0.5rem; margin-top: 1rem; }
.row-actions { display: flex; gap: 0.75rem; align-items: center; white-space: nowrap; }
.row-actions form { display: inline; margin: 0; }
.link-danger { background: none; border: none; color: #b91c1c; cursor: pointer; padding: 0;
               font: inherit; text-decoration: underline; }
.form-row { display: grid; gap: 0.3rem; margin-bottom: 1rem; max-width: 40rem; }
.form-row label { font-size: 0.85rem; font-weight: 600; }
.form-row input, .form-row textarea, .form-row select {
    font: inherit; padding: 0.45rem 0.6rem; border: 1px solid #d1d5db; border-radius: 6px;
    background: white; color: inherit; width: 100%; box-sizing: border-box; }
.form-row input[type=checkbox] { width: auto; }
.form-row input[readonly] { background: #f3f4f6; color: #6b7280; }
.form-row .req { color: #b91c1c; }
.actions { display: flex; gap: 0.75rem; margin-top: 1.5rem; }
pre { white-space: pre-wrap; word-break: break-word; font-family: ui-monospace, monospace;
      font-size: 0.85em; margin: 0.5rem 0 0; }
@media (prefers-color-scheme: dark) {
    body { background: #0f172a; color: #e2e8f0; }
    nav { border-bottom-color: #334155; }
    nav a:hover, tr:hover td { background: rgba(255,255,255,0.05); }
    nav a.current { background: rgba(255,255,255,0.1); }
    .card, th, td { border-color: #334155; }
    th { background: rgba(255,255,255,0.03); }
    .muted, .sub, .card .label { color: #94a3b8; }
    .err { color: #fca5a5; }
    .filters a { border-color: #334155; }
    .filters a.active { background: #f8fafc; color: #0f172a; border-color: #f8fafc; }
    .btn { border-color: #334155; }
    .btn:hover { background: rgba(255,255,255,0.06); }
    .btn.primary { background: #e2e8f0; color: #0f172a; border-color: #e2e8f0; }
    .btn.primary:hover { background: #f8fafc; }
    .link-danger { color: #fca5a5; }
    .form-row input, .form-row textarea, .form-row select { background: #1e293b; border-color: #334155; }
    .form-row input[readonly] { background: #0b1220; color: #94a3b8; }
}
`

// writePage emits a complete HTML document. Title is the page-level
// title, pageName is the subheading shown above the content. The nav is
// built from cfg.PathPrefix (so a custom prefix links correctly) and
// includes one link per configured entity. pageName is matched against
// the nav labels to mark the current item.
func (b *Battery) writePage(w http.ResponseWriter, title, pageName string, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s · %s</title>
  <link rel="stylesheet" href="%s/admin.css">
</head>
<body>
  <header>
    <h1>%s</h1>
    <p class="sub">%s</p>
  </header>
  %s
  %s
</body>
</html>`,
		render.Escape(title), render.Escape(pageName), b.cfg.PathPrefix,
		render.Escape(title), render.Escape(pageName), b.navHTML(pageName), body)
}

// handleCSS serves the admin stylesheet. Long-cacheable: the CSS is a build
// constant, not per-request data.
func (b *Battery) handleCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(w, baseCSS)
}

// navHTML builds the admin nav. The fixed Overview/Queue/Audit links plus
// one link per configured entity, all under cfg.PathPrefix. current is the
// page name; the matching link gets the .current class.
func (b *Battery) navHTML(current string) render.HTML {
	type link struct{ label, href string }
	links := []link{
		{"Overview", b.cfg.PathPrefix},
		{"Queue", b.cfg.PathPrefix + "/queue"},
		{"Audit log", b.cfg.PathPrefix + "/audit"},
	}
	if b.registry != nil {
		for _, name := range b.cfg.Entities {
			ent, err := b.registry.Get(name)
			if err != nil {
				continue
			}
			links = append(links, link{ent.GetName(), b.cfg.PathPrefix + "/e/" + ent.GetTable()})
		}
	}
	var sb strings.Builder
	sb.WriteString(`<nav>`)
	for _, l := range links {
		cls := ""
		if l.label == current {
			cls = ` class="current"`
		}
		fmt.Fprintf(&sb, `<a%s href="%s">%s</a>`, cls, render.Escape(l.href), render.Escape(l.label))
	}
	sb.WriteString(`</nav>`)
	return render.HTML(sb.String())
}

func section(name string, body render.HTML) render.HTML {
	return render.Raw(fmt.Sprintf(`<section><h2>%s</h2>%s</section>`,
		render.Escape(name), body))
}

func queueSummary(prefix string, stats queue.JobStats, wired bool) render.HTML {
	if !wired {
		return render.Raw(`<p class="muted">No queue wired.</p>`)
	}
	keys := []string{"pending", "claimed", "failed", "running", "dead"}
	var seen = map[string]bool{}
	cards := strings.Builder{}
	cards.WriteString(`<div class="cards">`)
	for _, k := range keys {
		if n, ok := stats[k]; ok {
			cards.WriteString(card(k, n))
			seen[k] = true
		}
	}
	// Emit any unexpected status names the queue produced.
	for k, n := range stats {
		if !seen[k] {
			cards.WriteString(card(k, n))
		}
	}
	cards.WriteString(`</div>`)
	cards.WriteString(fmt.Sprintf(`<p><a href="%s/queue">View all jobs →</a></p>`,
		render.Escape(prefix)))
	return render.Raw(cards.String())
}

func auditSummary(prefix string, total int, wired bool) render.HTML {
	if !wired {
		return render.Raw(`<p class="muted">No audit log wired.</p>`)
	}
	return render.Raw(fmt.Sprintf(`<div class="cards">%s</div>
		<p><a href="%s/audit">View recent entries →</a></p>`,
		card("entries", total), render.Escape(prefix)))
}

func card(label string, value int) string {
	return fmt.Sprintf(`<div class="card"><div class="label">%s</div><div class="value">%d</div></div>`,
		render.Escape(label), value)
}

func queueFilters(prefix, current string, stats queue.JobStats) render.HTML {
	all := []string{"", "pending", "claimed", "failed", "dead"}
	var b strings.Builder
	b.WriteString(`<div class="filters">`)
	for _, k := range all {
		cls := ""
		if k == current {
			cls = " class=\"active\""
		}
		label := k
		if label == "" {
			label = "all"
		}
		count := ""
		if k != "" {
			if n, ok := stats[k]; ok {
				count = fmt.Sprintf(" (%d)", n)
			}
		}
		q := ""
		if k != "" {
			q = "?status=" + k
		}
		fmt.Fprintf(&b, `<a%s href="%s/queue%s">%s%s</a>`,
			cls, render.Escape(prefix), q, render.Escape(label), count)
	}
	b.WriteString(`</div>`)
	return render.Raw(b.String())
}

func jobsTable(jobs []queue.Job) render.HTML {
	if len(jobs) == 0 {
		return render.Raw(`<p class="muted">No jobs match this filter.</p>`)
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr>
		<th>ID</th><th>Type</th><th>Attempts</th><th>Priority</th>
		<th>Created</th><th>Scheduled</th>
	</tr></thead><tbody>`)
	for _, j := range jobs {
		fmt.Fprintf(&b, `<tr>
			<td><code>%s</code></td>
			<td>%s</td>
			<td>%d / %d</td>
			<td>%d</td>
			<td>%s</td>
			<td>%s</td>
		</tr>`,
			render.Escape(j.ID),
			render.Escape(j.Type),
			j.Attempts, j.MaxAttempts,
			j.Priority,
			render.Escape(j.CreatedAt.Format(time.RFC3339)),
			render.Escape(j.ScheduledAt.Format(time.RFC3339)),
		)
	}
	b.WriteString(`</tbody></table>`)
	return render.Raw(b.String())
}

func auditTable(rows []auditRow) render.HTML {
	if len(rows) == 0 {
		return render.Raw(`<p class="muted">No audit entries yet.</p>`)
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr>
		<th>Time</th><th>Entity</th><th>Op</th><th>Record</th><th>Actor</th>
	</tr></thead><tbody>`)
	for _, r := range rows {
		actor := "—"
		if r.ActorID.Valid && r.ActorID.String != "" {
			actor = r.ActorID.String
		}
		fmt.Fprintf(&b, `<tr>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td><code>%s</code></td>
			<td>%s</td>
		</tr>`,
			render.Escape(r.CreatedAt.Format(time.RFC3339)),
			render.Escape(r.Entity),
			render.Escape(r.Op),
			render.Escape(r.RecordID),
			render.Escape(actor),
		)
	}
	b.WriteString(`</tbody></table>`)
	return render.Raw(b.String())
}

func parseLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > 1000 {
		n = 1000
	}
	return n
}
