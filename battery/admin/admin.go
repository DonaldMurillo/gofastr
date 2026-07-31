// Package admin is the back-office battery for GoFastr apps — stock operator
// screens on top of the data and controls the framework already exposes. It is
// NOT read-only: today it owns queue/audit ops dashboards, full entity CRUD
// (proxied through each entity's own CrudHandler so validation, owner/tenant
// scope, hooks, and audit apply exactly as on the public JSON API), the
// role→permission and user→role RBAC screens, and the process-module operator
// lifecycle (enable / disable / bump-generation / per-grant revoke).
//
// Every surface — SSR screens and RPC/form routes alike — is behind the admin
// default-deny gate (b.gate), which requires an authenticated admin (role
// "admin" by default, or whatever Config.Authorize decides). Unauthenticated
// requests are 401 (or redirected to Config.LoginPath); authenticated-but-
// unauthorized are 403. There is no unauthenticated or self-service path.
//
// The ops dashboards (queue, audit) are self-contained server-rendered HTML
// that work without a UI host. The entity CRUD screens render THROUGH the host's
// mounted UI host (runtime.js hydration, islands) and require one. All inputs
// that reach a SQL surface are bound as parameters, never interpolated; role,
// permission, and module names from operator forms are $n-bound in their stores.
//
// Every mutation (grant, revoke, assign-roles, module lifecycle, entity write)
// records a security/compliance audit row; a failed audit write is logged, not
// swallowed, because the mutation is already durable.
package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	html "github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/ui"
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

	// Theme supplies the shared design tokens (--color-* / --font-*) the admin
	// renders from, so the back-office matches the surface that mounts it
	// instead of looking like a separate tool. When zero, the framework
	// DefaultTheme is used. Pass the same theme the app's UI host uses for a
	// coherent experience; override any token to restyle.
	Theme style.Theme

	// FontFaceCSS is raw @font-face CSS for the app's fonts. The admin renders
	// standalone pages (its own <head>), so without this it would reference the
	// theme's --font-* families but never load their files. Pass the same
	// @font-face rules the UI host serves so the admin loads identical fonts.
	FontFaceCSS string

	// Queue is the optional Browsable queue. When set, /admin/queue is
	// active and appears in overview/navigation. When nil, it is hidden;
	// the direct page returns a "no queue wired" diagnostic so the route
	// never 404s ambiguously.
	Queue queue.Browsable

	// DB is the database connection used to read the audit log table and,
	// when entity admin is enabled, overrides the app DB for those operations.
	// When nil, Init uses the app DB; without either, /admin/audit returns a
	// "no audit log wired" stub.
	DB *sql.DB

	// AuditTable is the audit log table name. Defaults to "audit_log".
	AuditTable string

	// QueueListLimit caps rows on /admin/queue. Default 200.
	QueueListLimit int

	// AuditListLimit caps rows on /admin/audit. Default 200.
	AuditListLimit int

	// Entities lists the entity names to expose as editable CRUD screens
	// under <PathPrefix>/e/<table>. When empty (default) NOTHING is
	// exposed — an admin dropped into an app must name what it manages.
	// Set AllEntities for the whole-back-office behavior. Naming an
	// entity explicitly also works for a CRUD=false one, if you really
	// mean to. Screens proxy to each entity's own CrudHandler, so
	// validation, owner/tenant scope, hooks, and events all apply exactly
	// as on the JSON API.
	Entities []string

	// AllEntities exposes EVERY registered entity whose CRUD is enabled —
	// the explicit "generate the whole back-office" opt-in (previously the
	// implicit default when Entities was empty). CRUD-disabled entities
	// (e.g. battery/auth's users/sessions, which ship CRUD=false) are
	// skipped, so this never exposes credential tables. Ignored when
	// Entities is non-empty.
	AllEntities bool

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

	// LoginPath, when set, redirects an UNAUTHENTICATED GET to a configured
	// login page (`LoginPath?next=<requested path>`) instead of returning a
	// bare 401. An authenticated user lacking the admin role still gets 403 —
	// they're signed in, just not allowed. Empty (default) keeps the 401.
	LoginPath string

	// Policy is the RBAC role policy the admin screens manage. When set
	// alongside GrantStore, the role→permission matrix screen is active
	// at <PathPrefix>/rbac/roles and grant/revoke persists across restarts.
	Policy *access.RolePolicy

	// GrantStore persists role→permission grants to the database. When
	// set alongside Policy, grant/revoke via the admin screens writes to
	// both the live policy and the DB. Wire it with
	// framework.NewGrantStore(db, policy) + EnsureSchema + LoadInto at boot.
	GrantStore *access.GrantStore

	// Auth is the auth manager used for the user→role assignment screen.
	// When set, the user roles screen is active at <PathPrefix>/rbac/users.
	// The underlying UserStore must implement UserLister (for listing) and
	// UpdateRoles (for assignment) — EntityUserStore does both.
	Auth *auth.AuthManager

	// EffectiveRoles optionally resolves additional roles for the user roles
	// screen. Direct auth_users.roles are always included with origin "direct";
	// hook results are unioned with them and labeled by their supplied origin.
	// When nil, the screen keeps its direct-roles-only rendering.
	EffectiveRoles func(ctx context.Context, userID string) []access.RoleWithOrigin

	// ProcessModules is the process-module supervisor the operator lifecycle
	// screen manages. When set, /admin/modules is active: list every module's
	// state (404-vs-503 surfaced in copy) plus enable/disable, bump-generation
	// (the circuit-reset / recovery lever, design §8), and per-grant revoke.
	// When nil, the screen is not mounted and the route 404s. Wire it with
	// app.ProcessModules() — the real *framework.ProcessModuleSupervisor
	// satisfies the processModuleController interface.
	ProcessModules processModuleController

	// Logger receives operational warnings a handler can't surface to the
	// caller — notably a security/compliance audit-row write failing AFTER
	// its mutation already committed (grant, revoke, role-assign, module
	// lifecycle). The mutation is not rolled back (it took effect), but a
	// lost audit record is a silent security gap, so it MUST be logged.
	// Default: slog.Default(). Inject a *slog.Logger (e.g. the app's) so the
	// line lands wherever the host's structured logs go.
	Logger *slog.Logger
}

// Battery is the framework Battery implementation.
type Battery struct {
	cfg      Config
	app      *framework.App      // source of fully wired entity CRUD handlers
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
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Battery{cfg: cfg, db: cfg.DB, flash: newFlashStore()}
}

// logger returns the configured *slog.Logger, falling back to slog.Default()
// for a Battery constructed outside New (tests that build the struct by hand).
func (b *Battery) logger() *slog.Logger {
	if b.cfg.Logger != nil {
		return b.cfg.Logger
	}
	return slog.Default()
}

// authorized reports whether the current request may use the admin. The
// default (no Authorize configured) requires an authenticated user that holds
// the configured AdminRole — secure by default. A custom Authorize overrides
// the role check entirely.
func (b *Battery) authorized(ctx context.Context) bool {
	// BEFORE the custom hook, not after. A host that supplies its own
	// Authorize — which is exactly what the comment below recommends for a
	// different role model — otherwise gets no embed refusal at all, and past
	// this gate every admin route runs under a wildcard access policy. Whether
	// an embed may drive the back office is not the host's call to make.
	if _, embedded := embed.GrantFromContext(ctx); embedded {
		return false
	}
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

// ReservedEmbedPrefixes reports the prefix this battery actually mounted, so
// an embed grant can never reach the back office even when the app relocated
// it with Config.PathPrefix. See framework.EmbedReserving.
func (b *Battery) ReservedEmbedPrefixes() []string {
	if b.cfg.PathPrefix == "" {
		return []string{"/admin"}
	}
	return []string{b.cfg.PathPrefix}
}

// Init implements framework.Battery. Mounts the three admin pages on
// the App's router under cfg.PathPrefix.
func (b *Battery) Init(app *framework.App) error {
	b.app = app
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
	r.Post(b.cfg.PathPrefix+"/queue/_replay/{id}", guard(b.handleQueueReplay))
	r.Get(b.cfg.PathPrefix+"/audit", guard(b.handleAudit))

	// RBAC management screens + RPC routes. Same admin gate as every other
	// surface — an authenticated non-admin gets 403 on both the GET screens
	// and the POST RPCs. Wired only when Policy/GrantStore/Auth are set.
	if b.cfg.Policy != nil {
		r.Get(b.cfg.PathPrefix+"/rbac/roles", guard(b.handleRBACRoles))
	}
	if b.cfg.Auth != nil {
		r.Get(b.cfg.PathPrefix+"/rbac/users", guard(b.handleRBACUsers))
	}
	if b.cfg.GrantStore != nil {
		r.Post(b.cfg.PathPrefix+"/rbac/_grant", guard(b.handleRBACGrant))
		r.Post(b.cfg.PathPrefix+"/rbac/_revoke", guard(b.handleRBACRevoke))
	}
	if b.cfg.Auth != nil {
		r.Post(b.cfg.PathPrefix+"/rbac/_assign", guard(b.handleRBACAssign))
	}
	// Process-module operator lifecycle screen + POST actions. Same admin
	// gate as every other surface; wired only when a controller is set.
	if b.cfg.ProcessModules != nil {
		r.Get(b.cfg.PathPrefix+"/modules", guard(b.handleProcessModules))
		r.Post(b.cfg.PathPrefix+"/modules/_enable", guard(b.handleModuleEnable))
		r.Post(b.cfg.PathPrefix+"/modules/_disable", guard(b.handleModuleDisable))
		r.Post(b.cfg.PathPrefix+"/modules/_bump", guard(b.handleModuleBump))
		r.Post(b.cfg.PathPrefix+"/modules/_revoke", guard(b.handleModuleRevoke))
	}
}

// gate wraps a route handler so it refuses unauthorized callers (401). The
// framework auth chain sets the user; b.authorized decides. Used for the
// standalone ops pages and the entity RPC/form routes.
func (b *Battery) gate(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.authorized(r.Context()) {
			status := b.authzStatus(r.Context())
			// Unauthenticated GET → bounce to the login page (if configured)
			// with a next= back here, instead of a dead-end 401.
			if status == http.StatusUnauthorized && b.cfg.LoginPath != "" && r.Method == http.MethodGet {
				http.Redirect(w, r, b.cfg.LoginPath+"?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
				return
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		// The admin is a fully-trusted back-office gated above by its own
		// Authorize. Run its CRUD with a superuser policy so per-entity access
		// RBAC (e.g. PII scoping like "customers:read") doesn't lock the admin
		// out of the very entities it exists to manage.
		next(w, r.WithContext(adminSuperuserCtx(r.Context())))
	})
}

// adminSuperuserCtx installs an access policy granting the Wildcard permission,
// so every EntityConfig.Access gate the admin's CRUD hits passes. Safe because
// the request already cleared the admin Authorize gate.
func adminSuperuserCtx(ctx context.Context) context.Context {
	p := access.NewRolePolicy()
	p.Grant("__admin", access.Wildcard)
	ctx = access.WithPolicy(ctx, p)
	return access.WithRoles(ctx, []string{"__admin"})
}

// ----- handlers ------------------------------------------------------------

func (b *Battery) handleIndex(w http.ResponseWriter, r *http.Request) {
	var stats queue.JobStats
	if b.cfg.Queue != nil {
		stats, _ = b.cfg.Queue.Stats(r.Context())
	}
	var auditCount int
	db := b.effectiveDB()
	if db != nil {
		_ = db.QueryRowContext(r.Context(),
			fmt.Sprintf("SELECT COUNT(*) FROM %s", b.cfg.AuditTable),
		).Scan(&auditCount)
	}
	var sections []render.HTML
	if b.cfg.Queue != nil {
		sections = append(sections, adminSection("Queue", queueSummary(b.cfg.PathPrefix, stats, true)))
	}
	sections = append(sections, adminSection("Audit log", auditSummary(b.cfg.PathPrefix, auditCount, db != nil)))
	b.writePage(w, b.cfg.Title, "Overview", ui.Stack(ui.StackConfig{Gap: ui.GapLG}, sections...))
}

func (b *Battery) handleQueue(w http.ResponseWriter, r *http.Request) {
	if b.cfg.Queue == nil {
		b.writePage(w, b.cfg.Title, "Queue",
			ui.Muted(render.Text("No queue is wired into this admin battery.")))
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.QueueListLimit)
	jobs, err := b.cfg.Queue.ListJobs(r.Context(), status, limit)
	if err != nil {
		// Don't echo err.Error() — driver text leaks DSNs, IPs, secrets.
		b.writePage(w, b.cfg.Title, "Queue",
			adminError("Could not load queue jobs. Check the server logs for details."))
		return
	}
	stats, _ := b.cfg.Queue.Stats(r.Context())
	// Offer per-row Replay only on the failed view and only when the backend
	// supports replay (DBQueue does; memory/redis don't yet).
	showReplay := false
	if status == "failed" {
		if _, ok := b.cfg.Queue.(queue.Replayable); ok {
			showReplay = true
		}
	}
	body := ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		queueFilters(b.cfg.PathPrefix, status, stats),
		jobsTable(jobs, b.cfg.PathPrefix, middleware.TokenFromContext(r.Context()), showReplay),
	)
	b.writePage(w, b.cfg.Title, "Queue", body)
}

// handleQueueReplay re-queues a dead-lettered job. Mutating + gated: it is
// registered behind b.gate (admin-only) and the form carries the CSRF token —
// an ungated replay would be a privilege-escalation / job-amplification vector.
func (b *Battery) handleQueueReplay(w http.ResponseWriter, r *http.Request) {
	rq, ok := b.cfg.Queue.(queue.Replayable)
	if !ok {
		http.Error(w, "queue does not support replay", http.StatusNotImplemented)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}
	if err := rq.Replay(r.Context(), id); err != nil {
		http.Error(w, "replay failed; check server logs", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, b.cfg.PathPrefix+"/queue?status=failed", http.StatusSeeOther)
}

func (b *Battery) handleAudit(w http.ResponseWriter, r *http.Request) {
	if b.effectiveDB() == nil {
		b.writePage(w, b.cfg.Title, "Audit log",
			ui.Muted(render.Text("No DB / audit table is wired into this admin battery.")))
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), b.cfg.AuditListLimit)
	rows, err := b.queryAudit(r.Context(), limit)
	if err != nil {
		// Don't echo err.Error() — driver text leaks DSNs, schema, secrets.
		b.writePage(w, b.cfg.Title, "Audit log",
			adminError("Could not load audit rows. Check the server logs for details."))
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
	rows, err := b.effectiveDB().QueryContext(ctx, q)
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

// The standalone ops pages (queue / audit / rbac / modules / overview) skip
// the host UI host: they emit their own HTML document and pull ALL styling
// from the single registry-served stylesheet (handleCSS → registry.All()),
// exactly the battery/setup pattern. There is NO bespoke CSS string here —
// every visual comes from a registered component (DataTable, StatCard,
// FilterToolbar, Tag, Button, …) or from the registered ui-admin sheet
// (styles.go), which also owns the .layout-admin shell these pages reuse.

// writePage emits a complete standalone HTML document. The shell reuses the
// same .layout-admin / .layout-body / .layout-content skeleton the entity
// screens receive from the host, so the registered ui-admin sheet styles it
// without a single bespoke rule here. pageName is matched against the nav
// labels to mark the current item.
func (b *Battery) writePage(w http.ResponseWriter, title, pageName string, body render.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	header := ui.PageHeader(ui.PageHeaderConfig{Title: title, Subtitle: pageName})
	inner := fmt.Sprintf(`<div class="layout-body">%s<main class="layout-content">%s%s</main></div>`,
		b.navHTML(pageName), header, body)
	shell := render.Tag("div", map[string]string{
		"class":         "layout-admin",
		"data-fui-comp": "ui-admin",
	}, render.Raw(inner))
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s · %s</title>
  <link rel="stylesheet" href="%s/admin.css">
</head>
<body class="admin-standalone">
%s
</body>
</html>`, render.Escape(title), render.Escape(pageName), b.cfg.PathPrefix, shell)
}

// handleCSS serves the combined admin stylesheet: theme :root tokens + the
// CSS for EVERY registered component (ui-admin plus every framework/ui
// component the ops pages compose — DataTable, StatCard, FilterToolbar, Tag,
// Button, …). registry.All() is the single styling surface, so the admin
// ships zero bespoke CSS strings. Mirrors battery/setup's serveCSS.
func (b *Battery) handleCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	theme := b.cfg.Theme
	if theme.Colors.Background.Value == "" {
		theme = style.DefaultTheme()
	}
	var sb strings.Builder
	sb.WriteString(theme.CSSCustomProperties())
	sb.WriteString("\n")
	if b.cfg.FontFaceCSS != "" {
		sb.WriteString(b.cfg.FontFaceCSS)
		sb.WriteString("\n")
	}
	for _, e := range registry.All() {
		sb.WriteString(e.CSSFor(theme))
		sb.WriteString("\n")
	}
	_, _ = fmt.Fprint(w, sb.String())
}

// navHTML builds the admin nav as ui.Link action targets inside a <nav>. The
// current page's link carries aria-current="page"; the active styling comes
// from a scoped rule in the registered ui-admin sheet. Queue appears only
// with real backing; Overview/Audit and configured entities remain fixed.
func (b *Battery) navHTML(current string) render.HTML {
	type link struct{ label, href string }
	links := []link{{"Overview", b.cfg.PathPrefix}}
	if b.cfg.Queue != nil {
		links = append(links, link{"Queue", b.cfg.PathPrefix + "/queue"})
	}
	links = append(links, link{"Audit log", b.cfg.PathPrefix + "/audit"})
	if b.registry != nil {
		byName := b.registry.All()
		for _, name := range b.cfg.Entities {
			ent, ok := byName[name]
			if !ok {
				continue
			}
			links = append(links, link{ent.GetName(), b.cfg.PathPrefix + "/e/" + ent.GetTable()})
		}
	}
	if b.cfg.Policy != nil {
		links = append(links, link{"Roles", b.cfg.PathPrefix + "/rbac/roles"})
	}
	if b.cfg.Auth != nil {
		links = append(links, link{"User roles", b.cfg.PathPrefix + "/rbac/users"})
	}
	if b.cfg.ProcessModules != nil {
		links = append(links, link{"Modules", b.cfg.PathPrefix + "/modules"})
	}
	items := make([]render.HTML, 0, len(links))
	for _, l := range links {
		cfg := ui.LinkConfig{Href: l.href, Text: l.label, Variant: ui.LinkAction}
		if l.label == current {
			cfg.ExtraAttrs = html.Attrs{"aria-current": "page"}
		}
		items = append(items, ui.Link(cfg))
	}
	return render.Tag("nav", map[string]string{"class": "admin-nav"}, items...)
}

// adminSection wraps a titled content block via the design-system ui.Section
// (replaces the old hand-rolled <section><h2> helper).
func adminSection(heading string, body render.HTML) render.HTML {
	return ui.Section(ui.SectionConfig{Heading: heading}, body)
}

// adminError renders a page-level error flash as a ui.Callout (inline danger
// alert — the danger variant carries role=alert automatically). Replaces the
// old <p class="err"> markup.
func adminError(msg string) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(msg))
}

// queueSummary renders the overview's queue tile: a responsive grid of
// StatCards (one per status the queue reports) + a muted "view all" link.
func queueSummary(prefix string, stats queue.JobStats, wired bool) render.HTML {
	if !wired {
		return ui.Muted(render.Text("No queue wired."))
	}
	order := []string{"pending", "claimed", "failed", "running", "dead"}
	cards := make([]render.HTML, 0, len(stats))
	seen := make(map[string]bool, len(stats))
	for _, k := range order {
		if n, ok := stats[k]; ok {
			cards = append(cards, statCard(k, n))
			seen[k] = true
		}
	}
	// Emit any unexpected status names the queue produced.
	for k, n := range stats {
		if !seen[k] {
			cards = append(cards, statCard(k, n))
		}
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		ui.Grid(ui.GridConfig{Min: "9rem"}, cards...),
		ui.Link(ui.LinkConfig{Href: prefix + "/queue", Text: "View all jobs →", Variant: ui.LinkMuted}),
	)
}

// auditSummary renders the overview's audit tile.
func auditSummary(prefix string, total int, wired bool) render.HTML {
	if !wired {
		return ui.Muted(render.Text("No audit log wired."))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		ui.Grid(ui.GridConfig{Min: "9rem"}, statCard("entries", total)),
		ui.Link(ui.LinkConfig{Href: prefix + "/audit", Text: "View recent entries →", Variant: ui.LinkMuted}),
	)
}

// statCard renders one metric tile. label is the small caption, value the
// prominent number — matching ui.StatCard's Label/Value semantics.
func statCard(label string, value int) render.HTML {
	return ui.StatCard(ui.StatCardConfig{Label: label, Value: strconv.Itoa(value)})
}

// queueFilters renders the queue status filter as a ui.FilterToolbar — the
// design-system's URL-driven (GET <form>) facet control. A single pill facet
// over status; submitting navigates to ?status=<value>, so it works on these
// standalone pages with zero JavaScript.
func queueFilters(prefix, current string, stats queue.JobStats) render.HTML {
	// DBQueue's terminal state is 'failed', in-progress 'claimed' — it never
	// writes 'dead', so a 'dead' chip was a permanently-empty filter.
	opts := []ui.FacetOption{{Label: "All", Value: ""}}
	for _, k := range []string{"pending", "claimed", "failed"} {
		label := k
		if n, ok := stats[k]; ok {
			label = fmt.Sprintf("%s (%d)", k, n)
		}
		opts = append(opts, ui.FacetOption{Label: label, Value: k})
	}
	return ui.FilterToolbar(ui.FilterToolbarConfig{
		Action:     prefix + "/queue",
		ApplyLabel: "Filter",
		Facets: []ui.Facet{{
			Name:    "status",
			Label:   "Status",
			Kind:    ui.FacetPills,
			Value:   current,
			Options: opts,
		}},
	})
}

// jobsTable renders the job list as a ui.DataTable. When showReplay is true
// (failed-jobs view on a Replayable queue), each row's Actions cell carries a
// CSRF-protected Replay form posting to the gated /queue/_replay/{id} route.
func jobsTable(jobs []queue.Job, prefix, csrfToken string, showReplay bool) render.HTML {
	cols := []ui.Column{
		{Key: "id", Header: "ID"},
		{Key: "type", Header: "Type"},
		{Key: "attempts", Header: "Attempts"},
		{Key: "priority", Header: "Priority"},
		{Key: "created", Header: "Created"},
		{Key: "scheduled", Header: "Scheduled"},
	}
	if showReplay {
		cols = append(cols, ui.Column{Key: "actions", Header: "Actions"})
	}
	rows := make([]ui.Row, len(jobs))
	for i, j := range jobs {
		cells := map[string]render.HTML{
			"id":        monoCell(j.ID),
			"type":      render.Text(j.Type),
			"attempts":  render.Text(fmt.Sprintf("%d / %d", j.Attempts, j.MaxAttempts)),
			"priority":  render.Text(strconv.Itoa(j.Priority)),
			"created":   render.Text(j.CreatedAt.Format(time.RFC3339)),
			"scheduled": render.Text(j.ScheduledAt.Format(time.RFC3339)),
		}
		if showReplay {
			cells["actions"] = render.HTML(html.Form(html.FormConfig{
				Method: "post",
				Action: prefix + "/queue/_replay/" + url.PathEscape(j.ID),
			},
				html.Input(html.InputConfig{Type: "hidden", Name: "_csrf", Value: csrfToken}),
				ui.Button(ui.ButtonConfig{Label: "Replay", Type: "submit", Size: ui.ButtonSizeSmall}),
			))
		}
		rows[i] = ui.Row{Cells: cells}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns: cols,
		Rows:    rows,
		Empty:   ui.EmptyStateConfig{Title: "No jobs", Description: "No jobs match this filter.", HeadingLevel: 3},
	})
}

// auditTable renders the audit log as a ui.DataTable.
func auditTable(rows []auditRow) render.HTML {
	cols := []ui.Column{
		{Key: "time", Header: "Time"},
		{Key: "entity", Header: "Entity"},
		{Key: "op", Header: "Op"},
		{Key: "record", Header: "Record"},
		{Key: "actor", Header: "Actor"},
	}
	data := make([]ui.Row, len(rows))
	for i, r := range rows {
		actor := "—"
		if r.ActorID.Valid && r.ActorID.String != "" {
			actor = r.ActorID.String
		}
		data[i] = ui.Row{Cells: map[string]render.HTML{
			"time":   render.Text(r.CreatedAt.Format(time.RFC3339)),
			"entity": render.Text(r.Entity),
			"op":     render.Text(r.Op),
			"record": monoCell(r.RecordID),
			"actor":  render.Text(actor),
		}}
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns: cols,
		Rows:    data,
		Empty:   ui.EmptyStateConfig{Title: "No audit entries", Description: "Audit events will appear here.", HeadingLevel: 3},
	})
}

// monoCell renders text in the admin's quiet monospace cell style (ids, codes)
// via the existing .admin-mono class in the registered ui-admin sheet.
func monoCell(s string) render.HTML {
	return render.Tag("span", map[string]string{"class": "admin-mono"}, render.Text(s))
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
