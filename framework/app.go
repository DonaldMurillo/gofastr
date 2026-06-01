package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/cron"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// Mountable is anything that can register routes on the framework's router.
// UI hosts, admin panels, websocket pubsub layers, etc. all satisfy this
// interface and are attached via App.Mount.
type Mountable interface {
	Mount(*router.Router)
}

// JSONCase / CaseCamel / CaseSnake moved to framework/crud — see
// reexports_crud.go for the facade aliases that keep framework.X working.

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name           string        // application name
	JSONCase       crud.JSONCase // JSON key casing: "camelCase" (default) or "snake_case"
	DebugEndpoints bool          // opt-in for /.debug/* endpoints
	NoLLMMD        bool          // disable auto-generated /llm.md entity docs

	// RequestTimeout caps the per-request wall-clock budget enforced by
	// the default middleware chain. Zero (default) installs a 30s cap.
	// Set a positive duration to override. To disable the timeout
	// middleware entirely, set DisableRequestTimeout — overloading
	// sign for "disable" is too easy to trip on (e.g. accidentally
	// subtracting two timestamps).
	RequestTimeout time.Duration

	// DisableRequestTimeout removes the Timeout middleware from the
	// default chain entirely. Useful for long-running uploads / SSE;
	// pair with per-handler ctx deadlines if you still need bounded
	// request lifetime.
	DisableRequestTimeout bool
}

// App is the top-level application container.
// It wires together the entity registry, router, MCP server, and database.
type App struct {
	Registry *Registry
	router   *router.Router // access via App.Router() method
	MCP      *mcp.Server
	DB       *sql.DB
	Config   AppConfig
	Plugins  *PluginManager
	Storage  upload.Storage // optional; enables multipart on Image/File fields

	Batteries *BatteryManager

	serverMu   sync.Mutex   // guards server — Start writes, Shutdown reads/nils
	server     *http.Server
	events     *event.EventBus
	hooks      map[string]*hook.HookRegistry
	mountables []Mountable
	noDefaults bool

	// logger is the App-local *slog.Logger. Read via Logger(), swapped via
	// SetLogger(). Stored behind an atomic pointer so middleware composed
	// at NewApp time can resolve the current logger per request without a
	// lock — plugins (battery/log, etc.) can swap it from their Init.
	logger atomic.Pointer[slog.Logger]

	// initialized guards InitPlugins against double-init. Public so a test
	// or CLI can call InitPlugins manually pre-Start, then Start also calls
	// it — the second call must be a no-op (otherwise routes/middleware
	// double-register and panic on duplicate mux patterns).
	initialized atomic.Bool

	// Lifecycle hooks fired by Start/Stop. startHooks run with the app's
	// derived context so workers cancel when Stop is called.
	startHooks []func(ctx context.Context) error
	appCtx     context.Context
	appCancel  context.CancelFunc

	// lc is the graceful-shutdown coordinator. OnStop hooks register
	// here as Drainers so a single Shutdown() call walks both the
	// app-level stop hooks and any battery-registered drainers /
	// health checkers through one documented sequence.
	lc *lifecycle.Lifecycle

	// Readiness checks registered by app code, plugins, and batteries.
	// /readyz runs them in parallel; /healthz is unconditional.
	health *healthState

	// Feature flags. Lazily created on first access (Flags()) so apps
	// that never use them pay nothing.
	flagEval     *featureflag.Evaluator
	flagMu       sync.Mutex
	flagAccessed bool // true once Flags()/SetFlagStore has run — guards against late SetFlagStore

	// Optional idempotency config wired into the default chain when set.
	idempotency *middleware.IdempotencyConfig

	// Optional translator. When set, the i18n middleware is wired into
	// the default chain so handlers can call App.T(ctx, key, ...).
	translator *i18n.Translator

	// mcpIntrospection enables a set of read-only MCP tools that expose
	// the app's routes, plugins, batteries, config, and readiness state
	// for agent debugging. Set via WithMCPIntrospection().
	mcpIntrospection bool
}

// AppOption is a functional option for configuring an App.
type AppOption func(*App)

// WithDB sets the database connection.
func WithDB(db *sql.DB) AppOption {
	return func(a *App) {
		a.DB = db
	}
}

// WithConfig sets the application config.
func WithConfig(config AppConfig) AppOption {
	return func(a *App) {
		a.Config = config
	}
}

// WithRouter sets a custom router.
func WithRouter(r *router.Router) AppOption {
	return func(a *App) {
		a.router = r
	}
}

// Router returns the App's *router.Router for advanced use (plugin authors,
// batteries that need to register routes with custom matching, sub-router
// construction). Application code should prefer the App-level helpers:
//
//   - App.Use(mw)        — register middleware  (instead of Router().Use)
//   - App.Get/Post/...   — register routes      (instead of Router().Handle)
//   - App.Group(prefix)  — sub-routes           (instead of Router().Group)
//
// Both forms are functionally equivalent — App.Use forwards to Router().Use —
// but the App-level surface is the canonical one in docs and examples.
//
// Exposed as a method (rather than a field) so plugins and batteries can
// swap or wrap the router during Init without callers depending on direct
// field assignment.
func (a *App) Router() *router.Router { return a.router }

// WithMCPServer sets a custom MCP server.
func WithMCPServer(s *mcp.Server) AppOption {
	return func(a *App) {
		a.MCP = s
	}
}

// WithFileStorage sets the default upload.Storage used by CRUD handlers
// to persist files for Image and File entity fields when a multipart
// request arrives. Without this option, multipart requests on those
// fields fail with a clear error.
func WithFileStorage(s upload.Storage) AppOption {
	return func(a *App) {
		a.Storage = s
	}
}

// WithoutDefaultMiddleware disables the default middleware chain
// (recovery, request-id, logging, security headers, timeout). Use this
// when you want full control over middleware composition via Use().
func WithoutDefaultMiddleware() AppOption {
	return func(a *App) {
		a.noDefaults = true
	}
}

// WithLogger sets the App's *slog.Logger. Same effect as calling
// App.SetLogger after NewApp; available as an option for symmetry.
// Panics if l is nil — the App's logger is always non-nil; pass a
// discard logger (slog.New(slog.DiscardHandler)) if you want to
// silence output.
func WithLogger(l *slog.Logger) AppOption {
	if l == nil {
		panic("framework: WithLogger(nil) — use slog.DiscardHandler to silence logging")
	}
	return func(a *App) {
		a.logger.Store(l)
	}
}

// WithIdempotency adds an Idempotency-Key middleware to the default
// chain. Pass middleware.IdempotencyConfig{} to take all defaults; the
// option is otherwise idiomatic-Go composition over the existing
// middleware.Idempotency primitive.
//
// Has no effect when WithoutDefaultMiddleware is also set — wire your
// own chain explicitly in that case.
func WithIdempotency(cfg middleware.IdempotencyConfig) AppOption {
	return func(a *App) {
		a.idempotency = &cfg
	}
}

// WithI18n installs a Translator and wires its locale-negotiation
// middleware into the default chain. Handlers downstream can call
// App.T(ctx, key, ...) for translated strings driven by the caller's
// Accept-Language. Also installed as i18n.Default() so the package-
// level i18n.T helper works from anywhere.
//
// Panics when paired with WithoutDefaultMiddleware — register the
// middleware explicitly in your custom chain in that case.
func WithI18n(tr *i18n.Translator) AppOption {
	return func(a *App) {
		a.translator = tr
		i18n.SetDefault(tr)
	}
}

// Logger returns the App-local *slog.Logger. Middleware and plugins
// should call this — not slog.Default() — so that a logging plugin can
// replace the destination without rewiring globals.
//
// Always non-nil. NewApp seeds the App with a JSON-to-stderr logger
// that is independent of slog.Default; an unrelated slog.SetDefault
// elsewhere in the process does not redirect this App's logs.
func (a *App) Logger() *slog.Logger {
	return a.logger.Load()
}

// SetLogger replaces the App's logger. Atomic; safe to call
// concurrently with in-flight requests — atomic.Pointer.Store is
// race-free, and middleware reading via App.Logger() sees the new
// value on the next request.
//
// Panics if l is nil — the App's logger is always non-nil; pass a
// discard logger (slog.New(slog.DiscardHandler)) to silence output.
func (a *App) SetLogger(l *slog.Logger) {
	if l == nil {
		panic("framework: App.SetLogger(nil) — use slog.DiscardHandler to silence logging")
	}
	a.logger.Store(l)
}

// DefaultMiddleware is the framework's standard safety chain in
// canonical order:
//
//	recovery → request-id → [idempotency] → [i18n] → security headers → timeout
//
// The optional entries are present when the App was configured with
// WithIdempotency / WithI18n; the timeout entry is omitted when
// AppConfig.DisableRequestTimeout is true.
//
// Access logging is deliberately NOT in this list. battery/log owns
// structured access logging when registered, and ad-hoc apps that just
// want a basic line can add middleware.LoggingFn(app.Logger) themselves
// — having both fire produces duplicate entries with mismatched fields
// (`request` from the framework, `http.access` from the plugin).
//
// Takes the App so the recovery middleware can route panics through
// app.Logger (late-binding) and the timeout reflects
// AppConfig.RequestTimeout. Pass nil only in tests; the recovery falls
// back to slog.Default and the timeout to 30s.
func DefaultMiddleware(a *App) []router.Middleware {
	var getLogger func() *slog.Logger
	timeout := 30 * time.Second
	var idempotency *middleware.IdempotencyConfig
	var translator *i18n.Translator
	if a != nil {
		getLogger = a.Logger
		if a.Config.RequestTimeout > 0 {
			timeout = a.Config.RequestTimeout
		}
		idempotency = a.idempotency
		translator = a.translator
	}
	chain := []router.Middleware{
		middleware.RecoveryFn(getLogger),
		middleware.RequestID(),
	}
	if idempotency != nil {
		chain = append(chain, middleware.Idempotency(*idempotency))
	}
	if translator != nil {
		chain = append(chain, i18n.Middleware(translator))
	}
	chain = append(chain, middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}))
	if a == nil || !a.Config.DisableRequestTimeout {
		chain = append(chain, middleware.Timeout(timeout))
	}
	return chain
}

// Mount attaches a Mountable and registers its routes on the app's router
// immediately. The default middleware chain is already in place (committed
// during NewApp), so any handler the Mountable registers is wrapped with
// it. Returns the app for fluent chaining.
//
// Mountables typically register a NotFound catch-all (e.g. a UI host that
// renders pages for any unrouted path), so call Mount AFTER any explicit
// routes you want to take precedence (entity CRUD, custom endpoints).
//
// IMPORTANT — ordering with plugins/batteries: plugin.Init runs at
// App.Start (or InitPlugins), AFTER Mount has already registered the
// Mountable's routes. If a plugin's Init registers a more-specific
// route that overlaps with a Mountable's NotFound catch-all, the
// plugin's route still wins (ServeMux dispatches by specificity, not
// by registration order). But if a plugin registers a Mountable-style
// catch-all itself, it shadows any user routes added after Mount but
// before InitPlugins. Mount last unless you know what you're doing.
func (a *App) Mount(m Mountable) *App {
	a.mountables = append(a.mountables, m)
	m.Mount(a.router)
	return a
}

// Group creates a route group with the given prefix and optional configuration.
// The group supports its own middleware stack, access policy, OpenAPI tags,
// and MCP namespacing. Nested groups compose prefixes and middleware.
//
//	api := app.Group("/api")
//	api.Use(authMiddleware)
//	api.Get("/health", healthHandler)
//
//	admin := app.Group("/admin", routegroup.WithAccess(access.RequirePermission("admin:access")))
//	admin.Entity("settings", settingsConfig)
func (a *App) Group(prefix string, opts ...routegroup.GroupOption) *routegroup.RouteGroup {
	return routegroup.New(a.router, prefix, opts...)
}

// GroupEntity registers an entity with the given configuration inside a
// RouteGroup. CRUD routes mount at <group-prefix>/<entity-table>, MCP
// tools are namespaced under the group's MCPNamespace, and the OpenAPI
// tag reflects the group's OpenAPITag if set.
//
// This is the group-scoped equivalent of App.Entity.
func (a *App) GroupEntity(g *routegroup.RouteGroup, name string, config entity.EntityConfig) *App {
	e := entity.Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	if err := a.Registry.Register(e); err != nil {
		panic(fmt.Sprintf("framework: failed to register entity %q in group %q: %v", name, g.Prefix(), err))
	}

	crudEnabled := a.DB != nil && (config.CRUD == nil || *config.CRUD)
	if config.MCP && a.DB != nil && config.CRUD != nil && !*config.CRUD {
		panic(fmt.Sprintf("framework: entity %q has MCP=true with CRUD=false — MCP CRUD tools require the HTTP routes to be registered", name))
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		crudHandler = crud.NewCrudHandler(e, a.DB)
		crudHandler.JSONCase = a.JSONCasing()
		crudHandler.Hooks = a.HookRegistry(name)
		crudHandler.Storage = a.Storage
		crudHandler.Events = a.Events()
		crudHandler.Registry = a.Registry

		// Register CRUD routes on the group's sub-router.
		// The group's prefix is already baked into the sub-router,
		// so we just mount at /<entity-table>.
		crud.RegisterCrudRoutes(g.Router(), crudHandler, "/"+e.GetTable(), crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
	}

	// MCP tools — namespaced if the group has a namespace.
	if config.MCP && a.DB != nil {
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, g.Router()); err != nil {
			panic(fmt.Sprintf("framework: failed to register MCP tools for entity %q in group %q: %v", name, g.Prefix(), err))
		}
	}

	// Custom endpoints
	if len(config.Endpoints) > 0 {
		if err := a.registerGroupEndpoints(g, e, config.Endpoints); err != nil {
			panic(fmt.Sprintf("framework: failed to register endpoints for entity %q in group %q: %v", name, g.Prefix(), err))
		}
	}

	return a
}

// registerGroupEndpoints is the group-scoped equivalent of registerEntityEndpoints.
func (a *App) registerGroupEndpoints(g *routegroup.RouteGroup, ent *entity.Entity, endpoints []entity.Endpoint) error {
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		path := openapi.EntityEndpointPath(ent, endpoint.Path)
		if endpoint.Handler != nil {
			g.Handle(method, path, endpoint.Handler)
		}
		if endpoint.MCP {
			if endpoint.MCPHandler == nil {
				return fmt.Errorf("endpoint %q: MCPHandler is required when MCP is true", endpoint.Path)
			}
			toolName := endpoint.Name
			if toolName == "" {
				toolName = openapi.DefaultEndpointToolName(ent.GetName(), method, g.Prefix()+path)
			}
			if ns := g.MCPNamespace(); ns != "" {
				toolName = ns + "." + toolName
			}
			description := endpoint.Description
			if description == "" {
				description = method + " " + g.Prefix() + path
			}
			if err := a.MCP.RegisterTool(toolName, description, map[string]any{"type": "object"}, endpoint.MCPHandler); err != nil {
				return err
			}
		}
	}
	return nil
}

// Use appends middleware to the app's router chain. The default chain
// (installed by NewApp unless WithoutDefaultMiddleware is set) stays in
// place — Use adds to it, never silently replaces it. Plugins call Use
// from their Init to contribute middleware; router late-binding means
// these additions also wrap routes registered before the plugin loaded.
func (a *App) Use(mw ...router.Middleware) *App {
	if len(mw) == 0 {
		return a
	}
	a.router.Use(mw...)
	return a
}

// defaultDotEnvPaths returns the file list NewApp probes at boot, in
// precedence order (earlier wins). APP_ENV-specific file is included
// only when APP_ENV is set in the environment. Paths are relative to
// the process CWD; callers running gofastr from a non-project dir
// should set GOFASTR_DOTENV=off and call dotenv.LoadAndApply with
// explicit absolute paths.
func defaultDotEnvPaths() []string {
	paths := []string{".env.local"}
	if appEnv := os.Getenv("APP_ENV"); appEnv != "" {
		paths = append(paths, ".env."+appEnv)
	}
	return append(paths, ".env")
}

// NewApp creates a new App with the given options.
// It initializes default Registry, Router, and MCP Server if not provided.
func NewApp(opts ...AppOption) *App {
	// Auto-load .env files BEFORE option processing so options that
	// peek at os.Environ (WithDB("env://DATABASE_URL"), WithConfig
	// reading APP_ENV, etc.) see the merged values. Existing env
	// always wins — operator-set vars are not clobbered by dotfiles.
	//
	// File precedence (earlier wins on conflict): .env.local,
	// .env.<APP_ENV>, .env. Missing files are silent.
	//
	// Set GOFASTR_DOTENV=off in the real process env to suppress.
	// Callers that need custom paths should call dotenv.LoadAndApply
	// themselves before NewApp and set the off flag.
	if os.Getenv("GOFASTR_DOTENV") != "off" {
		_ = dotenv.LoadAndApply(defaultDotEnvPaths()...)
	}

	a := &App{
		Registry:  NewRegistry(),
		router:    router.New(),
		MCP:       mcp.NewServer(),
		Config:    AppConfig{JSONCase: crud.CaseCamel},
		Plugins:   NewPluginManager(),
		Batteries: NewBatteryManager(),
		events:    event.NewEventBus(),
		hooks:     make(map[string]*hook.HookRegistry),
		lc:        lifecycle.New(),
	}

	for _, opt := range opts {
		opt(a)
	}

	// Seed the App-local logger if no option supplied one. JSON to
	// stderr — independent of slog.Default so external slog rewiring
	// doesn't redirect this App's framework logs. battery/log replaces
	// it during Init via app.SetLogger.
	if a.logger.Load() == nil {
		a.logger.Store(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	// WithIdempotency / WithI18n add entries to the default chain; if
	// the caller also passed WithoutDefaultMiddleware they would never
	// appear. Surface the misconfiguration immediately rather than
	// silently dropping the middleware.
	if a.noDefaults && a.idempotency != nil {
		panic("framework: WithIdempotency is incompatible with WithoutDefaultMiddleware — " +
			"the idempotency middleware lives in the default chain; mount it explicitly via " +
			"router.Middleware(middleware.Idempotency(...)) in your custom chain instead")
	}
	if a.noDefaults && a.translator != nil {
		panic("framework: WithI18n is incompatible with WithoutDefaultMiddleware — " +
			"the i18n middleware lives in the default chain; mount it explicitly via " +
			"router.Middleware(i18n.Middleware(translator)) in your custom chain instead")
	}

	// Install the default middleware preset unless opted out. The router
	// resolves its middleware chain per request, so plugins can append
	// more via app.Use from their Init and still wrap routes that were
	// registered earlier (e.g. by Mount).
	if !a.noDefaults {
		a.router.Use(DefaultMiddleware(a)...)
	}

	// Propagate DB to registry and its entities
	if a.DB != nil {
		a.Registry.SetDB(a.DB)
	}

	// Auto-wire dev-only livereload routes. No-op unless GOFASTR_DEV=1 is
	// set (typically by `gofastr dev`) and the host isn't in production.
	// See framework/dev/livereload.go for the env-gate rules.
	dev.MaybeRegisterLiveReload(a.router)

	return a
}

// NewUIHostApp builds an App and mounts the given host on it in one call —
// the near-universal shape for SSR/UIHost apps, which otherwise repeat
//
//	app := framework.NewApp(opts...)
//	app.Mount(host)
//
// host is any Mountable (typically a *uihost.Host). Returns the App for
// fluent chaining.
func NewUIHostApp(host Mountable, opts ...AppOption) *App {
	return NewApp(opts...).Mount(host)
}

// Entity registers an entity with the given name and configuration.
// Returns the App for fluent chaining.
func (a *App) Entity(name string, config entity.EntityConfig) *App {
	// Registration-time validation: SeedFS without SeedPath is a
	// misconfiguration that would otherwise silently mark the entity
	// as seeded with empty data on first run.
	if config.SeedFS != nil && config.SeedPath == "" {
		panic(fmt.Sprintf("framework: entity %q has SeedFS set but SeedPath empty — point SeedPath at a file within the FS or unset SeedFS", name))
	}

	e := entity.Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	if err := a.Registry.Register(e); err != nil {
		panic(fmt.Sprintf("framework: failed to register entity %q: %v", name, err))
	}

	// Auto-register CRUD routes.
	// Default (CRUD==nil): auto-register when DB is set.
	// Set CRUD to &true to always register, &false to opt out.
	// MCP=true implies CRUD must be mounted: MCP tools dispatch through the
	// router so they share its middleware chain (auth, recovery, etc.).
	crudEnabled := a.DB != nil && (config.CRUD == nil || *config.CRUD)
	if config.MCP && a.DB != nil && config.CRUD != nil && !*config.CRUD {
		panic(fmt.Sprintf("framework: entity %q has MCP=true with CRUD=false — MCP CRUD tools require the HTTP routes to be registered", name))
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		crudHandler = crud.NewCrudHandler(e, a.DB)
		crudHandler.JSONCase = a.JSONCasing()
		crudHandler.Hooks = a.HookRegistry(name)
		crudHandler.Storage = a.Storage
		crudHandler.Events = a.Events()
		crudHandler.Registry = a.Registry
		crud.RegisterCrudRoutes(a.router, crudHandler, "/"+e.GetTable(), crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
	}

	if config.MCP && a.DB != nil {
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, a.router); err != nil {
			panic(fmt.Sprintf("framework: failed to register MCP tools for entity %q: %v", name, err))
		}
	}

	if len(config.Endpoints) > 0 {
		if err := a.registerEntityEndpoints(e, config.Endpoints); err != nil {
			panic(fmt.Sprintf("framework: failed to register endpoints for entity %q: %v", name, err))
		}
	}

	return a
}

// RegisterEntities registers each (name, config) pair via App.Entity in
// alphabetical-by-name order. Sorting matters: Entity has order-sensitive
// side effects — router registration, MCP tool list order, OpenAPI tag
// emission — and Go's map iteration is randomised, so unsorted iteration
// would mean non-deterministic /openapi.json bytes across restarts
// (breaking ETag caching) and non-deterministic MCP tools/list responses.
// FK relations stay safe because AutoMigrate also topologically sorts.
//
// Returns the App for fluent chaining.
//
//	app.RegisterEntities(map[string]entity.EntityConfig{
//	    "foods":  foodsConfig,
//	    "meals":  mealsConfig,
//	    "users":  usersConfig,
//	})
func (a *App) RegisterEntities(entities map[string]entity.EntityConfig) *App {
	names := make([]string, 0, len(entities))
	for name := range entities {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a.Entity(name, entities[name])
	}
	return a
}

// EntityFromFile loads and registers one JSON entity declaration.
func (a *App) EntityFromFile(path string) (*entity.Entity, error) {
	decl, err := entity.LoadEntityDeclaration(path)
	if err != nil {
		return nil, err
	}
	cfg, err := decl.Config()
	if err != nil {
		return nil, err
	}
	a.Entity(decl.Name, cfg)
	return a.Registry.Get(decl.Name)
}

// EntitiesFromDir loads and registers every *.json declaration in dir.
func (a *App) EntitiesFromDir(dir string) error {
	decls, err := entity.LoadEntityDeclarations(dir)
	if err != nil {
		return err
	}
	for _, decl := range decls {
		cfg, err := decl.Config()
		if err != nil {
			return err
		}
		a.Entity(decl.Name, cfg)
	}
	return nil
}

// GroupEntitiesFromDir loads every *.json declaration in dir and registers
// each one inside the given RouteGroup — the group-scoped equivalent of
// EntitiesFromDir. CRUD routes mount at <group-prefix>/<entity-table>,
// MCP tools are namespaced under the group's MCPNamespace.
//
// Use this with a /api group when every JSON entity should live behind
// a single prefix:
//
//	api := app.Group("/api")
//	app.GroupEntitiesFromDir(api, "entities")
func (a *App) GroupEntitiesFromDir(g *routegroup.RouteGroup, dir string) error {
	decls, err := entity.LoadEntityDeclarations(dir)
	if err != nil {
		return err
	}
	for _, decl := range decls {
		cfg, err := decl.Config()
		if err != nil {
			return err
		}
		a.GroupEntity(g, decl.Name, cfg)
	}
	return nil
}

func (a *App) registerEntityEndpoints(ent *entity.Entity, endpoints []entity.Endpoint) error {
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		path := openapi.EntityEndpointPath(ent, endpoint.Path)
		if endpoint.Handler != nil {
			a.router.Handle(method, path, endpoint.Handler)
		}
		if endpoint.MCP {
			if endpoint.MCPHandler == nil {
				return fmt.Errorf("endpoint %q: MCPHandler is required when MCP is true", endpoint.Path)
			}
			name := endpoint.Name
			if name == "" {
				name = openapi.DefaultEndpointToolName(ent.GetName(), method, path)
			}
			description := endpoint.Description
			if description == "" {
				description = method + " " + path
			}
			if err := a.MCP.RegisterTool(name, description, map[string]any{"type": "object"}, endpoint.MCPHandler); err != nil {
				return err
			}
		}
	}
	return nil
}

// openapi.EntityEndpointPath, convertColonParams, openapi.DefaultEndpointToolName moved to
// framework/openapi (where they're shared with the OpenAPI spec generator).

// JSONCasing returns the configured JSON casing strategy.
// Defaults to CaseCamel if not explicitly set.
func (a *App) JSONCasing() crud.JSONCase {
	if a.Config.JSONCase == "" {
		return crud.CaseCamel
	}
	return a.Config.JSONCase
}

// RegisterPlugin registers a plugin with the application's plugin manager.
// Returns the App for fluent chaining.
//
// Panics if InitPlugins has already run — plugins must be registered
// before App.Start (or the explicit InitPlugins call) so their Init
// fires. The panic is a clear contract violation rather than a silent
// no-op that would have the new plugin's routes / middleware vanish.
func (a *App) RegisterPlugin(plugin Plugin) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterPlugin(%q) called after InitPlugins; register plugins before App.Start", plugin.Name()))
	}
	if err := a.Plugins.Register(plugin); err != nil {
		panic(fmt.Sprintf("framework: failed to register plugin %q: %v", plugin.Name(), err))
	}
	return a
}

// RegisterBattery registers a heavyweight, lifecycle-aware battery module
// (auth, search, cache, etc.) with the application. deps lists battery names
// that must be initialized before this one. Returns the App for chaining.
//
// Batteries are initialized in dependency-resolved order during App.Start,
// before the HTTP server binds. Each battery's Init does whatever it needs
// by calling into the App (routes, middleware, hooks, MCP tools). Batteries
// that also implement BatteryLifecycle get their OnStart/OnStop fired by
// the App at the appropriate moment.
//
// Example:
//
//	app.RegisterBattery(auth.New(auth.Config{...}), "search") // depends on search battery
func (a *App) RegisterBattery(b Battery, deps ...string) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterBattery(%q) called after InitPlugins; register batteries before App.Start", b.Name()))
	}
	if err := a.Batteries.Register(b, deps...); err != nil {
		panic(fmt.Sprintf("framework: failed to register battery %q: %v", b.Name(), err))
	}
	return a
}

// InitPlugins initializes all registered plugins and batteries by calling
// their Init(app) method. Plugins go first (registration order), then
// batteries (dependency-resolved order). Each module does everything it
// needs from inside Init — register routes, add middleware, register MCP
// tools, attach hooks, swap the logger, etc.
//
// Idempotent: the first successful call latches an internal flag so any
// later call returns nil without re-running plugin Inits. This lets tests
// call InitPlugins() manually pre-Start without colliding with the
// implicit call inside Start.
func (a *App) InitPlugins() error {
	if !a.initialized.CompareAndSwap(false, true) {
		return nil
	}
	if err := a.Plugins.InitAll(a); err != nil {
		// Rollback the latch so the caller can retry after fixing
		// whatever caused the failure (otherwise a transient init
		// error would permanently brick the app).
		a.initialized.Store(false)
		return err
	}
	if err := a.Batteries.InitAll(a); err != nil {
		a.initialized.Store(false)
		return err
	}
	// Probe both plugins and batteries for the optional
	// ReadinessRegistrar interface so they can publish health checks
	// before /readyz mounts in Start.
	a.probeReadinessRegistrars()

	// Register introspection MCP tools if opted in. After plugin/battery
	// init so app_plugins / app_batteries reflect everything.
	if a.mcpIntrospection {
		if err := a.registerIntrospectionTools(); err != nil {
			return err
		}
	}

	return nil
}

// Events returns the application's event bus.
func (a *App) Events() *event.EventBus {
	if a.events == nil {
		a.events = event.NewEventBus()
	}
	return a.events
}

// HookRegistry returns (or creates) the hook registry for a named entity.
func (a *App) HookRegistry(entityName string) *hook.HookRegistry {
	if a.hooks == nil {
		a.hooks = make(map[string]*hook.HookRegistry)
	}
	if _, ok := a.hooks[entityName]; !ok {
		a.hooks[entityName] = hook.NewHookRegistry()
	}
	return a.hooks[entityName]
}

// OnStart registers a function to run once during App.Start, before the
// HTTP server begins accepting connections. The context passed in is
// cancelled when Stop is called, so workers should respect it.
//
// Hooks run in registration order; the first to return a non-nil error
// aborts Start.
func (a *App) OnStart(fn func(ctx context.Context) error) *App {
	a.startHooks = append(a.startHooks, fn)
	return a
}

// OnStop registers a function to run during App.Shutdown, after the
// HTTP server has shut down. Hooks run in reverse registration order
// — the last thing started is the first thing stopped. Internally the
// hook is wrapped as a lifecycle.Drainer so app-level cleanup and
// battery drains share one coordinator.
func (a *App) OnStop(fn func() error) *App {
	// LIFO ordering: prepend so reverse-of-Registration === drain order.
	a.lc.PrependDrainer(stopHookDrainer(fn))
	return a
}

// OnStopFirst registers an OnStop hook that runs LAST under the
// reverse-order Stop iteration. Useful for plugins (battery/log
// especially) that must outlive every other shutdown step: their
// close hook needs to fire AFTER every other OnStop has had a chance
// to emit log entries.
//
// Without this, a user that registers app.OnStop BEFORE
// RegisterPlugin(log) gets the order inverted on reverse iteration —
// log's close runs first, the user's OnStop logs into closed sinks.
func (a *App) OnStopFirst(fn func() error) *App {
	// Append so it runs LAST in the LIFO order used by PrependDrainer.
	a.lc.AppendDrainer(stopHookDrainer(fn))
	return a
}

// stopHookDrainer adapts a legacy OnStop func() error into the
// lifecycle.Drainer interface. The ctx is ignored — OnStop predates
// the context-aware drain API and is purely best-effort cleanup.
type stopHookDrainer func() error

func (f stopHookDrainer) Drain(_ context.Context) error { return f() }

// Lifecycle returns the App's graceful-shutdown coordinator. Batteries
// and plugins use Lifecycle().RegisterDrainer / RegisterHealthChecker
// to participate in Shutdown beyond the simple OnStop hook.
func (a *App) Lifecycle() *lifecycle.Lifecycle { return a.lc }

// RunWithSignals blocks until SIGINT or SIGTERM is received, then runs
// Shutdown. Returns Shutdown's error, or nil if ctx is cancelled before
// a signal arrives.
func (a *App) RunWithSignals(ctx context.Context) error {
	return a.lc.RunWithSignalsUsing(ctx, a.Shutdown)
}

// AddCron registers a Scheduler with the app's lifecycle: it starts when
// Start runs and stops when Stop runs. Returns the App for chaining so
// users can wire several schedulers in one expression.
func (a *App) AddCron(s *cron.Scheduler) *App {
	a.OnStart(func(ctx context.Context) error {
		s.Start(ctx)
		return nil
	})
	a.OnStop(func() error {
		s.Stop()
		return nil
	})
	return a
}

// schedulerStartStop is the minimal interface AddQueue needs. We keep it
// here (not in the queue package) so framework doesn't have to import
// battery/queue — apps wire their queue manually and just hand the
// start/stop pair over.
type schedulerStartStop interface {
	Start(ctx context.Context)
	Close() error
}

// AddQueue registers any queue/worker that exposes Start(ctx) and Close().
// The DBQueue from battery/queue satisfies this directly; in-memory and
// Redis variants can be wrapped.
func (a *App) AddQueue(q schedulerStartStop) *App {
	a.OnStart(func(ctx context.Context) error {
		q.Start(ctx)
		return nil
	})
	a.OnStop(func() error {
		return q.Close()
	})
	return a
}

// Shutdown gracefully stops the HTTP server, stops every registered
// battery in reverse dependency order, then runs each OnStop hook in
// reverse registration order. Matches net/http.Server.Shutdown's
// signature (takes a deadline ctx) but does the FULL lifecycle teardown.
// Safe to call multiple times — subsequent calls are no-ops.
//
// Call this from your signal handler.
func (a *App) Shutdown(ctx context.Context) error {
	if a.appCancel != nil {
		a.appCancel()
		a.appCancel = nil
	}
	var firstErr error
	a.serverMu.Lock()
	srv := a.server
	a.server = nil
	a.serverMu.Unlock()
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}

	// Stop batteries in reverse dependency order (dependents first)
	if err := a.Batteries.StopAll(ctx); err != nil && firstErr == nil {
		firstErr = err
	}

	// Run OnStop hooks + battery-registered drainers through the
	// lifecycle coordinator. PrependDrainer in OnStop already encodes
	// the reverse-of-registration order callers expect.
	if err := a.lc.Shutdown(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// runStartHooks fires every OnStart hook with the app's lifecycle context.
// Battery lifecycle hooks are called first, then app-level start hooks.
// Returns the first error so Start aborts cleanly before binding the port.
func (a *App) runStartHooks() error {
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}

	// Start batteries in dependency order
	if err := a.Batteries.StartAll(a.appCtx); err != nil {
		return err
	}

	// Then app-level start hooks (cron, queues, custom)
	for _, fn := range a.startHooks {
		if err := fn(a.appCtx); err != nil {
			return err
		}
	}
	return nil
}

// Start starts the HTTP server on the given address.
// Auto-migrates tables, registers OpenAPI/Swagger, debug stats, applies
// the default middleware chain (unless disabled), and calls Mount on
// every attached Mountable.
//
// Sets the process title to the app name for visibility in ps/Activity Monitor.
func (a *App) Start(addr string) error {
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		return fmt.Errorf("resolve isolation: %w", err)
	}
	addr, err = runtimeIsolation.Addr(addr)
	if err != nil {
		return fmt.Errorf("resolve isolated addr: %w", err)
	}

	// Create the app's lifecycle context early so AutoMigrate, RunSeeds,
	// InitPlugins, and runStartHooks all share a single cancellable
	// context. A failure in any of these phases calls Shutdown, which
	// cancels the context and drains any goroutines an earlier phase
	// spawned. Without this, a startHook that spawns a worker before a
	// later startHook fails would leak that worker past Start returning.
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}

	abort := func(err error) error {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = a.Shutdown(shutdownCtx)
		return err
	}

	// Auto-migrate all registered entities
	if a.DB != nil {
		if err := migrate.AutoMigrate(a.DB, a.Registry); err != nil {
			return abort(fmt.Errorf("auto-migrate: %w", err))
		}
		if err := migrate.RunSeeds(a.appCtx, a.DB, a.Registry); err != nil {
			return abort(fmt.Errorf("run seeds: %w", err))
		}
	}

	// Initialize plugins and batteries (routes, middleware, tools, hooks).
	// Must happen after auto-migrate (so entity tables exist) but before
	// start hooks (so batteries can reference their routes).
	if err := a.InitPlugins(); err != nil {
		return abort(fmt.Errorf("init plugins: %w", err))
	}

	// Run OnStart hooks (cron/queue workers, custom setup). Failure here
	// aborts before we bind the port — better than a half-up server.
	if err := a.runStartHooks(); err != nil {
		return abort(fmt.Errorf("start hooks: %w", err))
	}

	// Auto-generate and serve OpenAPI spec. Only when the app actually
	// declares entities — a UI-only app (e.g. a content site) has an
	// empty registry and gets none of these routes. The startup banner
	// below keys off the same flags so it never advertises a 404.
	hasAPI := len(a.Registry.All()) > 0
	hasLLMMD := false
	if hasAPI {
		appName := a.Config.Name
		if appName == "" {
			appName = "GoFastr API"
		}
		spec := openapi.EntityOpenAPI(a.Registry, appName, "1.0.0")
		a.router.Get("/openapi.json", coreoa.Handler(spec))
		a.router.Get("/api/docs/", coreoa.SwaggerUIHandler(spec, "/api/docs"))

		// API entity index under /api/ alongside /api/docs/ (Swagger).
		// Root /llm.md is free for the homepage screen doc.
		if !a.Config.NoLLMMD {
			a.router.Get("/api/llm.md", crud.RegistryLLMMDHandler(a.Registry, appName))
			hasLLMMD = true
		}
	}

	if a.Config.DebugEndpoints {
		a.registerDebugEndpoints()
	}

	// Auto-register a db readiness probe if a DB is configured. Plugins
	// and batteries that implement ReadinessRegistrar were given a chance
	// to add their own during InitPlugins above.
	if a.DB != nil {
		c := dbReadinessCheck(a)
		a.RegisterReadiness(c.Name, c.Check)
	}
	a.registerHealthEndpoints()

	// Mountables already registered their routes when App.Mount was called;
	// nothing to do at Start time.

	name := a.Config.Name
	if name == "" {
		name = "gofastr"
	}

	// Set process title so it shows in ps / Activity Monitor
	os.Args[0] = "gofastr-" + name

	// Strip leading colon for display
	host := addr
	if len(host) > 0 && host[0] == ':' {
		host = "localhost" + host
	}

	fmt.Printf("\n  %s %s server ready\n", bold("GoFastr"), name)
	fmt.Printf("  %s PID: %d\n", arrow(), os.Getpid())
	if a.Config.DebugEndpoints {
		fmt.Printf("  %s Stats: http://%s/.debug/stats\n", arrow(), host)
	}

	// Log entity routes
	for _, e := range a.Registry.All() {
		fmt.Printf("  %s %-12s http://%s/%s\n", arrow(), e.GetName(), host, e.GetTable())
	}

	// Log OpenAPI surfaces — only the ones actually mounted above, so the
	// banner never points at a route that 404s.
	if hasAPI {
		fmt.Printf("  %s OpenAPI:     http://%s/openapi.json\n", arrow(), host)
		fmt.Printf("  %s Swagger UI:  http://%s/api/docs/\n", arrow(), host)
		if hasLLMMD {
			fmt.Printf("  %s LLM Docs:    http://%s/api/llm.md\n", arrow(), host)
		}
	}
	fmt.Println()

	a.serverMu.Lock()
	srv := &http.Server{
		Addr:    addr,
		Handler: a.router,
		// Conservative defaults so a slow / abandoned / hostile client
		// can't tie up the listener forever (slowloris-style). Hosts
		// that need a different shape can wrap the server themselves.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	a.server = srv
	a.serverMu.Unlock()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// registerDebugEndpoints adds /.debug/stats for runtime diagnostics.
// The endpoint exposes process internals (pid, goroutines, memory) so it
// requires an authenticated caller — the framework's normal auth chain
// must set a user in context for the request to succeed.
func (a *App) registerDebugEndpoints() {
	a.router.Get("/.debug/stats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := handler.GetUser(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		stats := map[string]any{
			"app":        a.Config.Name,
			"pid":        os.Getpid(),
			"uptime":     time.Since(startTime).Round(time.Millisecond).String(),
			"goroutines": runtime.NumGoroutine(),
			"cpuCores":   runtime.NumCPU(),
			"goVersion":  runtime.Version(),
			"memory": map[string]any{
				"alloc":       formatBytes(m.Alloc),
				"totalAlloc":  formatBytes(m.TotalAlloc),
				"sys":         formatBytes(m.Sys),
				"heapAlloc":   formatBytes(m.HeapAlloc),
				"heapSys":     formatBytes(m.HeapSys),
				"heapInUse":   formatBytes(m.HeapInuse),
				"stackInUse":  formatBytes(m.StackInuse),
				"gcCycles":    m.NumGC,
				"gcPauseLast": fmt.Sprintf("%.3fms", float64(m.PauseNs[(m.NumGC+255)%256])/1e6),
			},
			"entities": len(a.Registry.All()),
			"jsonCase": string(a.JSONCasing()),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}))
}

var startTime = time.Now()

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func arrow() string        { return "\033[33m→\033[0m" }
func bold(s string) string { return "\033[1m" + s + "\033[0m" }
