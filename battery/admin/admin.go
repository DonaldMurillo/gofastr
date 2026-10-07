// Package admin is the back office for GoFastr apps: a dashboard, the
// entity screens, and the operations pages (jobs, audit log, roles, user
// roles, process modules), drawn in one shell through the app's UI host.
//
// Every page renders through the host's pipeline (runtime.js, client
// navigation, the toast stack) and composes the kit; the battery ships no
// CSS. The entity screens are framework/entityui's list and record, read
// in process under the caller's own context plus the admin's per-call
// elevation (crud.WithElevation), and written through routes the battery
// mounts behind its own gate.
//
// One default-deny gate covers every page and route: an authenticated
// caller holding Config.AdminRole ("admin"), or whatever Config.Authorize
// decides. A signed-out caller gets 401 (or a redirect to Config.LoginPath),
// a signed-in caller without the role 403. An embed grant and an access
// Decider's deny refuse the whole back office.
//
// Every mutation records an audit row. A failed audit write is logged,
// never swallowed, because the mutation already took effect.
package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Config configures the Admin battery.
type Config struct {
	// PathPrefix is the URL prefix every admin page and route mounts
	// under. Defaults to "/admin".
	PathPrefix string

	// Title is the product name in the sidebar and the page titles.
	// Defaults to the localized "Admin".
	Title string

	// Logo is an image URL drawn beside Title in the sidebar. Empty draws
	// Title's initial.
	Logo string

	// UI is the app's entity screens (App.EntityUI). Required when the
	// admin exposes any entity: the admin draws its lists and records
	// with it and points their writes at its own gated routes. The admin
	// reads every name it uses from the entities' Display config.
	UI *entityui.UI

	// Entities lists the entity names the admin exposes, under
	// <PathPrefix>/entities/<name>. Empty (the default) exposes none:
	// an admin dropped into an app names what it manages. Set AllEntities
	// for every entity with CRUD on. Naming an entity also exposes one
	// with CRUD off.
	Entities []string

	// AllEntities exposes every registered entity whose CRUD is on.
	// CRUD-off entities (battery/auth's users and sessions) stay hidden,
	// so it never exposes credential tables. Ignored when Entities is set.
	AllEntities bool

	// Queue is the job queue the Jobs page and the dashboard's failed
	// jobs card read. Nil leaves both out.
	Queue queue.Browsable

	// DB holds the audit log the Audit log page and the dashboard's
	// recent activity read, and the ops pages write. Defaults to the app's
	// DB. Entity reads and writes always go through the app's own CRUD
	// handlers.
	DB *sql.DB

	// AuditTable is the audit log table. Defaults to "audit_log".
	AuditTable string

	// QueueListLimit caps rows on the Jobs page. Default 200.
	QueueListLimit int

	// AuditListLimit caps rows on the Audit log page. Default 200.
	AuditListLimit int

	// SavedViews turns on per-user saved views over the admin's
	// database (Config.DB or the app's): one named filter/columns set
	// per user per entity, kept apart by owner and tenant. The store is
	// SavedViews(); the UI wiring is the host's (UI.WithSavedViews).
	SavedViews bool

	// SavedViewsTable is the saved views table. Defaults to
	// "admin_saved_views". Must be a lowercase identifier
	// ([a-z_][a-z0-9_]*).
	SavedViewsTable string

	// BulkJobs runs bulk actions over more records than one request may
	// touch, on a battery/queue backend. Build it with NewBulkJobs and
	// pass the same value to app.EntityUI's Extensions.Jobs; Init binds
	// it to Config.UI and hands back any job a crash left unenqueued.
	BulkJobs *BulkJobs

	// Authorize replaces the role check: it returns true to admit the
	// request. The embed refusal and a Decider's deny still run first.
	// Nil requires an authenticated user whose GetRoles() holds AdminRole.
	Authorize func(ctx context.Context) bool

	// AdminRole is the role the default check requires. Defaults to
	// "admin". Ignored when Authorize is set.
	AdminRole string

	// LoginPath, when set, sends a signed-out page request to
	// LoginPath?next=<path> instead of a 401. A signed-in caller without
	// the role still gets 403.
	LoginPath string

	// SignOutPath is where the account menu's Sign out posts. Defaults to
	// Auth's logout route when Auth is set; with neither, the menu has no
	// Sign out.
	SignOutPath string

	// Policy is the role policy the Roles page manages and the tier
	// checks read. With GrantStore the page grants and revokes.
	Policy *access.RolePolicy

	// GrantStore persists role grants. With Policy, Grant and Revoke
	// write both the live policy and the database. Wire it with
	// framework.NewGrantStore(db, policy), EnsureSchema and LoadInto.
	GrantStore *access.GrantStore

	// Auth backs the User roles page: its UserStore lists users and
	// updates their roles (EntityUserStore does both).
	Auth *auth.AuthManager

	// EffectiveRoles adds resolved roles to the User roles page. Direct
	// roles always show as "direct"; these are unioned with them and
	// labeled by their origin.
	EffectiveRoles func(ctx context.Context, userID string) []access.RoleWithOrigin

	// ProcessModules is the process-module supervisor the Modules page
	// manages (app.ProcessModules()). Nil leaves the page out.
	ProcessModules processModuleController

	// Pages are the app's own admin pages, drawn in the shell with
	// breadcrumbs and a palette entry.
	Pages []Page

	// Cards are the app's own dashboard cards.
	Cards []Card

	// Metrics are the figures in the strip at the top of the dashboard,
	// in order: counts and sums over exposed entities.
	Metrics []Metric

	// Links are extra sidebar links, each in a nav group.
	Links []Link

	// Commands are extra palette entries that go to a URL.
	Commands []ui.PaletteCommand

	// Logger receives what a handler cannot tell its caller, notably an
	// audit row that failed to write after its mutation committed.
	// Defaults to slog.Default().
	Logger *slog.Logger
}

// Battery is the framework Battery implementation.
type Battery struct {
	cfg    Config
	app    *framework.App
	db     *sql.DB // the audit log's database
	host   *uihost.UIHost
	router *router.Router
	ents   []*entity.Entity
	names  []string     // the names of ents, the set the admin elevates
	ui     *entityui.UI // cfg.UI with writes pointed at the admin's routes

	savedViews entityui.SavedViewStore // nil unless Config.SavedViews
}

// New constructs the Admin battery. Pass the result to
// framework.App.RegisterBattery.
func New(cfg Config) *Battery {
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "/admin"
	}
	cfg.PathPrefix = strings.TrimRight(cfg.PathPrefix, "/")
	if cfg.AuditTable == "" {
		cfg.AuditTable = "audit_log"
	}
	if cfg.QueueListLimit <= 0 {
		cfg.QueueListLimit = 200
	}
	if cfg.SavedViewsTable == "" {
		cfg.SavedViewsTable = "admin_saved_views"
	}
	if cfg.AuditListLimit <= 0 {
		cfg.AuditListLimit = 200
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SignOutPath == "" && cfg.Auth != nil {
		cfg.SignOutPath = cfg.Auth.Config().BasePath + "/logout"
	}
	return &Battery{cfg: cfg, db: cfg.DB}
}

// logger returns the configured logger.
func (b *Battery) logger() *slog.Logger {
	if b.cfg.Logger != nil {
		return b.cfg.Logger
	}
	return slog.Default()
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "admin" }

// ReservedEmbedPrefixes reports the prefix the battery mounted, so an
// embed grant can never reach the back office even when the app relocated
// it with Config.PathPrefix. See framework.EmbedReserving.
func (b *Battery) ReservedEmbedPrefixes() []string {
	if b.cfg.PathPrefix == "" {
		return []string{"/admin"}
	}
	return []string{b.cfg.PathPrefix}
}

// Init implements framework.Battery. It refuses, naming what is missing,
// an app with no UI host, an exposed entity without Config.UI, an icon
// the kit does not register, and a page whose path or build is unusable;
// then it mounts the shell, the pages and the routes.
func (b *Battery) Init(app *framework.App) error {
	b.app = app
	if b.db == nil {
		b.db = app.DB
	}
	for _, m := range app.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			b.host = h
			break
		}
	}
	if b.host == nil {
		return errors.New("admin: every admin page renders through the app's UI host; mount one first (framework.NewUIHostApp or uihost.New)")
	}
	b.router = app.Router()
	ents, err := b.entitiesToExpose()
	if err != nil {
		return err
	}
	b.ents = ents
	for _, e := range ents {
		b.names = append(b.names, e.GetName())
	}
	if len(b.ents) > 0 {
		if b.cfg.UI == nil {
			return fmt.Errorf("admin: exposing %s needs Config.UI (app.EntityUI(ext)): the admin draws entity screens with it", entityNames(b.ents))
		}
		b.ui = b.cfg.UI.WithAPIPath(func(e *entity.Entity) (string, bool) {
			if b.exposed(e) {
				return b.apiBase(e), true
			}
			return "", false
		}).WithRecordPath(func(e *entity.Entity) (string, bool) {
			if b.exposed(e) {
				return b.entityBase(e), true
			}
			return "", false
		})
	}
	if err := b.checkConfig(); err != nil {
		return err
	}
	if b.cfg.SavedViews {
		sv, err := newSavedViews(context.Background(), b.db, b.cfg.SavedViewsTable)
		if err != nil {
			return fmt.Errorf("admin: saved views: %w", err)
		}
		b.savedViews = sv
		if b.ui != nil {
			b.ui = b.ui.WithSavedViews(sv)
		}
	}
	if b.cfg.BulkJobs != nil {
		b.cfg.BulkJobs.setAdmit(b.admitJob)
		// Bind first: a job resumed before its handler was registered
		// would be dead-lettered by the queue as an unknown type.
		b.cfg.BulkJobs.Bind(b.cfg.UI)
		if n, err := b.cfg.UI.ResumeBulkJobs(context.Background(), bulkResumeGrace); err != nil {
			b.logger().Error("admin: resume bulk jobs", "resumed", n, "error", err)
		} else if n > 0 {
			b.logger().Info("admin: resumed bulk jobs", "count", n)
		}
	}
	b.mount()
	return nil
}

// SavedViews returns the saved views store, or nil when Config.SavedViews
// is off. Every method acts for the caller in ctx only; see
// entityui.SavedViewStore.
func (b *Battery) SavedViews() entityui.SavedViewStore {
	return b.savedViews
}

// checkConfig refuses the names and paths Init cannot draw.
func (b *Battery) checkConfig() error {
	for _, e := range b.ents {
		if n := navOf(e); n != nil {
			if err := checkIcon(n.Icon, "entity "+e.GetName()); err != nil {
				return err
			}
		}
	}
	seen := map[string]bool{}
	for i, p := range b.cfg.Pages {
		if !strings.HasPrefix(p.Path, "/") || strings.HasPrefix(p.Path, "//") || strings.ContainsAny(p.Path, "?#\\") {
			return fmt.Errorf("admin: Pages[%d].Path %q must be a path under the admin, starting with /", i, p.Path)
		}
		if reservedPagePath(p.Path) {
			return fmt.Errorf("admin: Pages[%d].Path %q is one of the admin's own paths", i, p.Path)
		}
		if seen[p.Path] {
			return fmt.Errorf("admin: Pages[%d].Path %q is used twice", i, p.Path)
		}
		seen[p.Path] = true
		if p.Build == nil {
			return fmt.Errorf("admin: Pages[%d] (%s) has no Build", i, p.Path)
		}
		if p.Title == "" {
			return fmt.Errorf("admin: Pages[%d] (%s) has no Title", i, p.Path)
		}
		if p.Nav != nil {
			if err := checkIcon(p.Nav.Icon, "page "+p.Path); err != nil {
				return err
			}
		}
	}
	keys := map[string]bool{}
	for i, c := range b.cfg.Cards {
		if !entity.ValidKey(c.Key) {
			return fmt.Errorf("admin: Cards[%d].Key %q must be a lowercase slug", i, c.Key)
		}
		if keys[c.Key] {
			return fmt.Errorf("admin: Cards[%d].Key %q is used twice", i, c.Key)
		}
		keys[c.Key] = true
		if c.Build == nil || c.Title == "" {
			return fmt.Errorf("admin: Cards[%d] (%s) needs a Title and a Build", i, c.Key)
		}
		if c.Poll < 0 {
			return fmt.Errorf("admin: Cards[%d] (%s) has a negative Poll", i, c.Key)
		}
	}
	for i, m := range b.cfg.Metrics {
		if err := b.checkMetric(m, ""); err != nil {
			return fmt.Errorf("admin: Metrics[%d]: %w", i, err)
		}
	}
	for i, l := range b.cfg.Links {
		if l.Label == "" || !strings.HasPrefix(l.Href, "/") || strings.HasPrefix(l.Href, "//") {
			return fmt.Errorf("admin: Links[%d] needs a Label and a same-origin Href", i)
		}
		if err := checkIcon(l.Icon, "link "+l.Label); err != nil {
			return err
		}
	}
	if err := b.checkFeatureConfig(); err != nil {
		return err
	}
	return nil
}

// checkFeatureConfig refuses the optional features' config Init cannot
// start: a saved views table name that is not a lowercase identifier,
// saved views with no database to keep them in, and bulk jobs with no
// UI to run them through.
func (b *Battery) checkFeatureConfig() error {
	if !savedViewsTableRe.MatchString(b.cfg.SavedViewsTable) {
		return fmt.Errorf("admin: SavedViewsTable %q must be a lowercase identifier ([a-z_][a-z0-9_]*)", b.cfg.SavedViewsTable)
	}
	if b.cfg.SavedViews && b.db == nil {
		return errors.New("admin: Config.SavedViews needs Config.DB or an app database")
	}
	if b.cfg.BulkJobs != nil && b.cfg.UI == nil {
		return errors.New("admin: Config.BulkJobs needs Config.UI (app.EntityUI with Extensions.Jobs = the same runner): the jobs run the entity screens' bulk actions")
	}
	return nil
}

// checkIcon refuses an icon name the kit does not register.
func checkIcon(name, owner string) error {
	if name != "" && !ui.IconRegistered(name) {
		return fmt.Errorf("admin: %s names icon %q, which the kit does not register (ui.RegisterIcon adds one)", owner, name)
	}
	return nil
}

// entityNames lists entity names for an error message.
func entityNames(ents []*entity.Entity) string {
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.GetName()
	}
	return strings.Join(names, ", ")
}

// ----- the gate --------------------------------------------------------------

// authorized reports whether the request may use the admin. The embed
// refusal and a Decider's deny run before everything else and cannot be
// lifted by Authorize; a Decider can veto the admin, never admit it.
func (b *Battery) authorized(ctx context.Context) bool {
	if _, embedded := embed.GrantFromContext(ctx); embedded {
		return false
	}
	if d := access.GetDecider(ctx); d != nil {
		if d(ctx, access.GetRoles(ctx), access.Wildcard, access.Ref{}) == access.DecisionDeny {
			return false
		}
	}
	if b.cfg.Authorize != nil {
		return b.cfg.Authorize(ctx)
	}
	// battery/auth seeds a nil user on every request, so ok alone is true
	// for an anonymous caller; the nil check is what refuses one.
	u, ok := handler.GetUser(ctx)
	if !ok || u == nil {
		return false
	}
	rh, ok := u.(interface{ GetRoles() []string })
	if !ok {
		return false
	}
	return slices.Contains(rh.GetRoles(), b.adminRole())
}

// admitJob is the gate a queued bulk run's rebuilt context passes, the
// same one the admin's bulk route applies: Config.Policy goes on a
// context that has none, and a creator the gate admits now runs
// elevated. A creator it no longer admits runs as a plain caller.
func (b *Battery) admitJob(ctx context.Context) context.Context {
	if b.cfg.Policy != nil && access.PolicyFromContext(ctx) == nil {
		ctx = access.WithPolicy(ctx, b.cfg.Policy)
	}
	if b.authorized(ctx) {
		return b.elevate(ctx)
	}
	return ctx
}

// elevate lifts the Access check of the entities the admin exposes and
// of no other: a relation, a hook or a stat reaching an entity the admin
// does not show reads as the caller. Call it only once the caller passed
// the gate.
func (b *Battery) elevate(ctx context.Context) context.Context {
	return crud.WithElevation(ctx, b.names...)
}

// adminRole returns the configured admin role, defaulting to "admin".
func (b *Battery) adminRole() string {
	if b.cfg.AdminRole != "" {
		return b.cfg.AdminRole
	}
	return "admin"
}

// authzStatus is 401 for a caller with no user, 403 for a signed-in one.
func (b *Battery) authzStatus(ctx context.Context) int {
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}

// routes is the router group every admin route mounts on, behind the
// gate.
func (b *Battery) routes() *router.Router {
	return b.router.Group("", func(next http.Handler) http.Handler { return b.gate(next) })
}

// headers wraps every answer under the prefix, the screens the host
// serves included, refusals too: the security headers whether or not the
// app kept its default middleware, and no-store, because every admin
// answer is per-caller data and a cookie-authenticated GET is outside
// RFC 9111's Authorization-only storage rules.
func (b *Battery) headers(next http.Handler) http.Handler {
	secure := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.underPrefix(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		secure.ServeHTTP(w, r)
	})
}

// gate refuses unauthorized callers and, for a state-changing method, a
// cross-site request. It is the battery's own CSRF posture at the one
// choke point every mutating route passes through; middleware.CSRF adds
// token checks on top when the host mounts it, but the battery does not
// rely on it.
func (b *Battery) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeAdminMethod(r.Method) && rejectCrossSiteForm(w, r) {
			return
		}
		if !b.authorized(r.Context()) {
			status := b.authzStatus(r.Context())
			if status == http.StatusUnauthorized && b.cfg.LoginPath != "" && r.Method == http.MethodGet {
				http.Redirect(w, r, b.cfg.LoginPath+"?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
				return
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isSafeAdminMethod reports a method that cannot mutate state, exempt
// from the cross-site refusal: a cross-site link to an admin page is
// legitimate.
func isSafeAdminMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// ----- ops posts -------------------------------------------------------------

// maxAdminBodyBytes caps an ops post body, the repo's 1 MiB form
// convention (battery/auth, the CSRF middleware, crud's Bind).
const maxAdminBodyBytes int64 = 1 << 20

// isRPC reports a post from the runtime's form RPC, which sends JSON. A
// plain form post (no script) sends urlencoded.
func isRPC(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// readOps reads an ops post's fields from either encoding, capped. A form
// RPC sends a field that appears once as a string and a repeated one as
// an array; any other JSON value is refused. On failure it has answered.
func (b *Battery) readOps(w http.ResponseWriter, r *http.Request, page string) (url.Values, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodyBytes)
	tooLarge := func(err error) bool {
		_, ok := errors.AsType[*http.MaxBytesError](err)
		return ok
	}
	if isRPC(r) {
		// Strict: a repeated or case-folded key reads one way here and
		// another on the form surface, so the body is refused.
		var raw map[string]any
		if err := handler.DecodeStrict(r.Body, &raw); err != nil {
			if tooLarge(err) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return nil, false
			}
			b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
			return nil, false
		}
		vals := url.Values{}
		for k, v := range raw {
			switch t := v.(type) {
			case string:
				vals.Set(k, t)
			case []any:
				for _, item := range t {
					s, ok := item.(string)
					if !ok {
						b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
						return nil, false
					}
					vals.Add(k, s)
				}
			default:
				b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
				return nil, false
			}
		}
		return vals, true
	}
	if err := r.ParseForm(); err != nil {
		if tooLarge(err) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil, false
		}
		b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
		return nil, false
	}
	return r.PostForm, true
}

// result is one outcome an ops post reports: its message and whether it
// succeeded. The names are the only values a page reads from ?result=,
// so a page never prints request text.
type result struct {
	key i18nui.Key
	ok  bool
}

var results = map[string]result{
	"done":               {i18nui.KeyAdminDone, true},
	"replayed":           {i18nui.KeyAdminReplayed, true},
	"replayed-all":       {i18nui.KeyAdminReplayedAll, true},
	"granted":            {i18nui.KeyAdminGranted, true},
	"revoked":            {i18nui.KeyAdminRevoked, true},
	"roles-saved":        {i18nui.KeyAdminRolesSaved, true},
	"module-enabled":     {i18nui.KeyAdminModuleEnabled, true},
	"module-disabled":    {i18nui.KeyAdminModuleDisabled, true},
	"module-bumped":      {i18nui.KeyAdminModuleBumped, true},
	"module-revoked":     {i18nui.KeyAdminModuleRevoked, true},
	"refused":            {i18nui.KeyAdminRefused, false},
	"failed":             {i18nui.KeyAdminFailed, false},
	"bad-input":          {i18nui.KeyAdminBadInput, false},
	"grant-refused":      {i18nui.KeyAdminGrantRefused, false},
	"assign-refused":     {i18nui.KeyAdminAssignRefused, false},
	"unknown-capability": {i18nui.KeyAdminUnknownCapability, false},
	"module-failed":      {i18nui.KeyAdminModuleFailed, false},
	"module-refused":     {i18nui.KeyAdminModuleRefused, false},
	"queue-load-failed":  {i18nui.KeyAdminQueueLoadFailed, false},
	"replay-unsupported": {i18nui.KeyAdminFailed, false},
}

// done answers a successful ops post. A form RPC gets 204 and a toast;
// the form's own navigation re-fetches the page. A plain post gets a 303
// back to page with ?result=name.
func (b *Battery) done(w http.ResponseWriter, r *http.Request, page, name string) {
	res := results[name]
	if isRPC(r) {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, Title: i18nui.T(r.Context(), res.key), TTL: 4000})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, withResult(page, name), http.StatusSeeOther)
}

// refuse answers a refused or failed ops post with status. A form RPC
// gets {"error": message}, which the runtime shows as a toast; a plain
// post gets a 303 back to page with ?result=name.
func (b *Battery) refuse(w http.ResponseWriter, r *http.Request, page string, status int, name string) {
	res := results[name]
	if isRPC(r) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": i18nui.T(r.Context(), res.key)})
		return
	}
	http.Redirect(w, r, withResult(page, name), http.StatusSeeOther)
}

// withResult appends ?result=name to page, keeping its query.
func withResult(page, name string) string {
	u, err := url.Parse(page)
	if err != nil {
		return page
	}
	q := u.Query()
	q.Set("result", name)
	u.RawQuery = q.Encode()
	return u.String()
}

// resultNotice draws a plain post's outcome, read back from ?result= on
// the page it returned to. An unknown name draws nothing.
func resultNotice(ctx context.Context) render.HTML {
	r := appui.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return ""
	}
	res, ok := results[r.URL.Query().Get("result")]
	if !ok {
		return ""
	}
	variant := ui.StatusSuccess
	if !res.ok {
		variant = ui.StatusDanger
	}
	return ui.Callout(ui.CalloutConfig{Variant: variant}, render.Text(i18nui.T(ctx, res.key)))
}
