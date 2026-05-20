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
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
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
}

// Battery is the framework Battery implementation.
type Battery struct {
	cfg Config
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
	return &Battery{cfg: cfg}
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "admin" }

// Init implements framework.Battery. Mounts the three admin pages on
// the App's router under cfg.PathPrefix.
func (b *Battery) Init(app *framework.App) error {
	b.RegisterRoutes(app.Router())
	return nil
}

// RegisterRoutes mounts the three admin pages under cfg.PathPrefix on
// the supplied router. Exposed so apps that compose their own router
// can mount the admin without going through the battery lifecycle.
func (b *Battery) RegisterRoutes(r *router.Router) {
	r.Get(b.cfg.PathPrefix, http.HandlerFunc(b.handleIndex))
	r.Get(b.cfg.PathPrefix+"/queue", http.HandlerFunc(b.handleQueue))
	r.Get(b.cfg.PathPrefix+"/audit", http.HandlerFunc(b.handleAudit))
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
	writePage(w, b.cfg.Title, "Overview", body)
}

func (b *Battery) handleQueue(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Queue == nil {
		writePage(w, b.cfg.Title, "Queue",
			render.Raw(`<p class="muted">No queue is wired into this admin battery.</p>`))
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.QueueListLimit)
	jobs, err := b.cfg.Queue.ListJobs(r.Context(), status, limit)
	if err != nil {
		writePage(w, b.cfg.Title, "Queue",
			render.Raw(fmt.Sprintf(`<p class="err">List error: %s</p>`,
				render.Escape(err.Error()))))
		return
	}
	stats, _ := b.cfg.Queue.Stats(r.Context())
	body := queueFilters(b.cfg.PathPrefix, status, stats) +
		jobsTable(jobs)
	writePage(w, b.cfg.Title, "Queue", body)
}

func (b *Battery) handleAudit(w http.ResponseWriter, r *http.Request) {
	if b.cfg.DB == nil {
		writePage(w, b.cfg.Title, "Audit log",
			render.Raw(`<p class="muted">No DB / audit table is wired into this admin battery.</p>`))
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.AuditListLimit)
	rows, err := b.queryAudit(r.Context(), limit)
	if err != nil {
		writePage(w, b.cfg.Title, "Audit log",
			render.Raw(fmt.Sprintf(`<p class="err">Query error: %s</p>`,
				render.Escape(err.Error()))))
		return
	}
	writePage(w, b.cfg.Title, "Audit log", auditTable(rows))
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
@media (prefers-color-scheme: dark) {
    body { background: #0f172a; color: #e2e8f0; }
    nav { border-bottom-color: #334155; }
    nav a:hover, tr:hover td { background: rgba(255,255,255,0.05); }
    .card, th, td { border-color: #334155; }
    th { background: rgba(255,255,255,0.03); }
    .muted, .sub, .card .label { color: #94a3b8; }
    .err { color: #fca5a5; }
    .filters a { border-color: #334155; }
    .filters a.active { background: #f8fafc; color: #0f172a; border-color: #f8fafc; }
}
`

// writePage emits a complete HTML document. Title is the page-level
// title, pageName is the subheading shown above the content.
func writePage(w http.ResponseWriter, title, pageName string, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s · %s</title>
  <style>%s</style>
</head>
<body>
  <header>
    <h1>%s</h1>
    <p class="sub">%s</p>
  </header>
  <nav>
    <a href="/admin">Overview</a>
    <a href="/admin/queue">Queue</a>
    <a href="/admin/audit">Audit log</a>
  </nav>
  %s
</body>
</html>`,
		render.Escape(title), render.Escape(pageName), baseCSS,
		render.Escape(title), render.Escape(pageName), body)
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
