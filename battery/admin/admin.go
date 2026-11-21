// Package admin is the back office for GoFastr apps: a dashboard, the entity
// screens and the ops pages, drawn in one gated shell through the app's UI
// host.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
)

// Config configures the Admin battery.
type Config struct {
	// PathPrefix is the URL prefix every admin page and route mounts under.
	// Defaults to "/admin".
	PathPrefix string

	// Title is the product name in the sidebar and the page titles. Defaults
	// to the localized "Admin".
	Title string

	// Logo is an image URL drawn beside Title in the sidebar. Empty draws
	// Title's initial.
	Logo string

	// UI is the app's entity screens (App.EntityUI), required when the admin
	// exposes any entity.
	UI *entityui.UI

	// Entities lists the entity names the admin exposes. Empty (the default)
	// exposes none.
	Entities []string

	// AllEntities exposes every registered entity whose CRUD is on. Ignored
	// when Entities is set.
	AllEntities bool

	// Themes are the looks the account page's Appearance card offers beside
	// the light and dark toggle.
	Themes []ui.ThemeChoice

	// Queue is the job queue the Jobs page and the dashboard's failed jobs
	// card read. Nil leaves both out.
	Queue queue.Browsable

	// DB holds the audit log and, with SavedViews, the saved views. Defaults
	// to the app's DB.
	DB *sql.DB

	// AuditTable is the audit log table. Defaults to "audit_log".
	AuditTable string

	// QueueListLimit is the Jobs page's rows per page. Default 50.
	QueueListLimit int

	// AuditListLimit is the Audit log page's rows per page. Default 50.
	AuditListLimit int

	// SavedViews enables the admin database's saved filters and columns, up to
	// entityui.SavedViewCap named views per user, tenant and entity.
	SavedViews bool

	// SavedViewsTable is the saved views table, a lowercase identifier.
	// Defaults to "admin_saved_views".
	SavedViewsTable string

	// BulkJobs runs bulk actions over more records than one request may touch;
	// pass the same value to app.EntityUI's Extensions.Jobs.
	BulkJobs *BulkJobs

	// Authorize replaces the role check; the embed, token-scope and Decider
	// refusals still run first.
	Authorize func(ctx context.Context) bool

	// AdminRole is the role the default check requires. Defaults to "admin".
	AdminRole string

	// LoginPath, when set, redirects a signed-out page request to
	// LoginPath?next=<path and query> instead of a 401.
	LoginPath string

	// SignOutPath is where the account menu's Sign out posts. Defaults to
	// Auth's logout route.
	SignOutPath string

	// Policy is the role policy the Roles page manages and the tier checks
	// read.
	Policy *access.RolePolicy

	// GrantStore persists role grants; with Policy the Roles page grants and
	// revokes.
	GrantStore *access.GrantStore

	// Auth backs the User roles page through its UserStore.
	Auth *auth.AuthManager

	// EffectiveRoles adds resolved roles, labeled by origin, to the User roles
	// page.
	EffectiveRoles func(ctx context.Context, userID string) []access.RoleWithOrigin

	// ProcessModules is the supervisor the Modules page manages. Nil leaves
	// the page out.
	ProcessModules processModuleController

	// Pages are the app's own admin pages, drawn in the shell.
	Pages []Page

	// Cards are the app's own dashboard cards.
	Cards []Card

	// Metrics are the dashboard strip's figures, at most six with a Queue's
	// Failed jobs figure among them.
	Metrics []Metric

	// Attention are the list views the dashboard's Needs attention panel
	// previews.
	Attention []Watch

	// DashboardNew names an exposed entity whose New button heads the
	// dashboard.
	DashboardNew string

	// Links are extra sidebar links, each in a nav group.
	Links []Link

	// Commands are extra palette entries with fixed labels and URLs.
	Commands []ui.PaletteCommand

	// EmailPreviews are the mails the Emails page renders with sample data.
	// Empty leaves the page out.
	EmailPreviews []EmailPreview

	// Logger receives what a handler cannot tell its caller, such as a failed
	// audit write. Defaults to slog.Default().
	Logger *slog.Logger
}

// Battery is the framework Battery implementation.
type Battery struct {
	cfg    Config
	app    *framework.App
	db     *sql.DB
	host   *uihost.UIHost
	router *router.Router
	ents   []*entity.Entity
	names  []string     // the names of ents, the set the admin elevates
	ui     *entityui.UI // cfg.UI with writes pointed at the admin's routes
	ops    []opsPage

	savedViews entityui.SavedViewStore
}

// New constructs the Admin battery for framework.App.RegisterBattery.
func New(cfg Config) *Battery {
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "/admin"
	}
	cfg.PathPrefix = strings.TrimRight(cfg.PathPrefix, "/")
	if cfg.AuditTable == "" {
		cfg.AuditTable = "audit_log"
	}
	if cfg.QueueListLimit <= 0 {
		cfg.QueueListLimit = 50
	}
	if cfg.SavedViewsTable == "" {
		cfg.SavedViewsTable = "admin_saved_views"
	}
	if cfg.AuditListLimit <= 0 {
		cfg.AuditListLimit = 50
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.SignOutPath == "" && cfg.Auth != nil {
		cfg.SignOutPath = cfg.Auth.Config().BasePath + "/logout"
	}
	return &Battery{cfg: cfg, db: cfg.DB}
}

func (b *Battery) logger() *slog.Logger {
	if b.cfg.Logger != nil {
		return b.cfg.Logger
	}
	return slog.Default()
}

// loadFailed logs a list read that failed and draws the screen's
// failure notice: a danger callout, under the page's header when the
// screen has one. logAttrs carry any extra log attributes.
func (b *Battery) loadFailed(ctx context.Context, label string, phrase i18n.Phrase, err error, header []render.HTML, logAttrs ...any) render.HTML {
	args := append(logAttrs, "error", err)
	b.logger().Error(label, args...)
	callout := ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(phrase.Text(ctx)))
	if len(header) == 0 {
		return callout
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, append(header, callout)...)
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "admin" }

// ReservedEmbedPrefixes reports the mounted prefix, so no embed grant reaches
// the back office.
func (b *Battery) ReservedEmbedPrefixes() []string {
	if b.cfg.PathPrefix == "" {
		return []string{"/admin"}
	}
	return []string{b.cfg.PathPrefix}
}

// Init implements framework.Battery. It errors on a config it cannot draw,
// then mounts the shell, the pages and the routes.
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
			if b.writable(e) {
				return b.apiBase(e), true
			}
			return "", false
		}).WithRecordPath(func(e *entity.Entity) (string, bool) {
			if b.exposed(e) {
				return b.entityBase(e), true
			}
			return "", false
		}).WithActorName(b.actorName)
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
		jobsUI := b.jobsUI()
		b.cfg.BulkJobs.Bind(jobsUI)
		if n, err := jobsUI.ResumeBulkJobs(context.Background(), entityui.BulkResumeGrace); err != nil {
			b.logger().Error("admin: resume bulk jobs", "resumed", n, "error", err)
		} else if n > 0 {
			b.logger().Info("admin: resumed bulk jobs", "count", n)
		}
	}
	b.mount()
	return nil
}

func (b *Battery) jobsUI() *entityui.UI {
	app := b.cfg.UI
	return app.WithAPIPath(func(e *entity.Entity) (string, bool) {
		if b.writable(e) {
			return b.apiBase(e), true
		}
		if b.exposed(e) {
			return "", false
		}
		return app.WritePath(e)
	})
}

// SavedViews returns the saved views store, or nil when Config.SavedViews is
// off.
func (b *Battery) SavedViews() entityui.SavedViewStore {
	return b.savedViews
}

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
		if p.Title.IsZero() {
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
		if c.Build == nil || c.Title.IsZero() {
			return fmt.Errorf("admin: Cards[%d] (%s) needs a Title and a Build", i, c.Key)
		}
		if c.Poll < 0 {
			return fmt.Errorf("admin: Cards[%d] (%s) has a negative Poll", i, c.Key)
		}
	}
	most := maxFigures
	if b.cfg.Queue != nil {
		most--
	}
	if len(b.cfg.Metrics) > most {
		return fmt.Errorf("admin: Metrics holds %d figures; the strip takes at most %d (%d, less one for the Failed jobs figure a Queue adds)", len(b.cfg.Metrics), most, maxFigures)
	}
	for i, m := range b.cfg.Metrics {
		if err := b.checkMetric(m, ""); err != nil {
			return fmt.Errorf("admin: Metrics[%d]: %w", i, err)
		}
	}
	if n := b.cfg.DashboardNew; n != "" {
		if _, ok := b.exposedNamed(n); !ok {
			return fmt.Errorf("admin: DashboardNew %q is not an entity the admin exposes", n)
		}
	}
	for i, w := range b.cfg.Attention {
		if err := b.checkWatch(w); err != nil {
			return fmt.Errorf("admin: Attention[%d] %w", i, err)
		}
	}
	for i, l := range b.cfg.Links {
		if l.Label.IsZero() || !strings.HasPrefix(l.Href, "/") || strings.HasPrefix(l.Href, "//") {
			return fmt.Errorf("admin: Links[%d] needs a Label and a same-origin Href", i)
		}
		if err := checkIcon(l.Icon, "link "+l.Label.English()); err != nil {
			return err
		}
	}
	if err := checkEmailPreviews(b.cfg.EmailPreviews); err != nil {
		return err
	}
	if err := b.checkFeatureConfig(); err != nil {
		return err
	}
	return nil
}

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

func checkIcon(name, owner string) error {
	if name != "" && !ui.IconRegistered(name) {
		return fmt.Errorf("admin: %s names icon %q, which the kit does not register (ui.RegisterIcon adds one)", owner, name)
	}
	return nil
}

func entityNames(ents []*entity.Entity) string {
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.GetName()
	}
	return strings.Join(names, ", ")
}

// refuseBodyError answers a failed ops-post body read through the ops
// envelope: 413 over the cap, 400 bad-input otherwise.
func (b *Battery) refuseBodyError(w http.ResponseWriter, r *http.Request, page string, err error) {
	if bodylimit.TooLarge(err) {
		b.refuse(w, r, page, http.StatusRequestEntityTooLarge, "body-too-large")
		return
	}
	b.refuse(w, r, page, http.StatusBadRequest, "bad-input")
}
