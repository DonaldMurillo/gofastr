package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gofastr/gofastr/core/openapi"

	"github.com/gofastr/gofastr/core/mcp"
	"github.com/gofastr/gofastr/core/middleware"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core/upload"
	"github.com/gofastr/gofastr/framework/cron"
	"github.com/gofastr/gofastr/framework/entity"
	"github.com/gofastr/gofastr/framework/event"
	"github.com/gofastr/gofastr/framework/hook"
)

// Mountable is anything that can register routes on the framework's router.
// UI hosts, admin panels, websocket pubsub layers, etc. all satisfy this
// interface and are attached via App.Mount.
type Mountable interface {
	Mount(*router.Router)
}

// JSONCase defines the casing convention for JSON keys in API responses.
type JSONCase string

const (
	// CaseCamel outputs camelCase (default, web standard).
	CaseCamel JSONCase = "camelCase"
	// CaseSnake outputs snake_case (database-style).
	CaseSnake JSONCase = "snake_case"
)

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name           string   // application name
	JSONCase       JSONCase // JSON key casing: "camelCase" (default) or "snake_case"
	DebugEndpoints bool     // opt-in for /.debug/* endpoints
}

// App is the top-level application container.
// It wires together the entity registry, router, MCP server, and database.
type App struct {
	Registry *Registry
	Router   *router.Router
	MCP      *mcp.Server
	DB       *sql.DB
	Config   AppConfig
	Plugins  *PluginManager
	Storage  upload.Storage // optional; enables multipart on Image/File fields

	server     *http.Server
	events     *event.EventBus
	hooks      map[string]*hook.HookRegistry
	mountables []Mountable
	mwApplied  bool
	noDefaults bool

	// Lifecycle hooks fired by Start/Stop. startHooks run with the app's
	// derived context so workers cancel when Stop is called.
	startHooks []func(ctx context.Context) error
	stopHooks  []func() error
	appCtx     context.Context
	appCancel  context.CancelFunc
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
		a.Router = r
	}
}

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

// Mount attaches a Mountable and registers its routes on the app's router
// immediately. The default middleware chain is already in place (committed
// during NewApp), so any handler the Mountable registers is wrapped with
// it. Returns the app for fluent chaining.
//
// Mountables typically register a NotFound catch-all (e.g. a UI host that
// renders pages for any unrouted path), so call Mount AFTER any explicit
// routes you want to take precedence (entity CRUD, custom endpoints).
func (a *App) Mount(m Mountable) *App {
	a.mountables = append(a.mountables, m)
	m.Mount(a.Router)
	return a
}

// Use appends middleware to the app's router. Calling Use disables the
// default middleware chain — once you start composing your own, the app
// trusts you to attach what you need.
func (a *App) Use(mw ...router.Middleware) *App {
	if len(mw) == 0 {
		return a
	}
	a.Router.Use(mw...)
	a.mwApplied = true
	return a
}

// applyDefaultMiddleware attaches the standard chain on first call. The
// order is outermost-first: recovery → request-id → logging → security
// headers → timeout. Skipped entirely when WithoutDefaultMiddleware is
// set or when Use has already been called.
func (a *App) applyDefaultMiddleware() {
	if a.mwApplied || a.noDefaults {
		return
	}
	a.Router.Use(
		router.Middleware(middleware.Recovery()),
		router.Middleware(middleware.RequestID()),
		router.Middleware(middleware.Logging()),
		router.Middleware(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})),
		router.Middleware(middleware.Timeout(30*time.Second)),
	)
	a.mwApplied = true
}

// NewApp creates a new App with the given options.
// It initializes default Registry, Router, and MCP Server if not provided.
func NewApp(opts ...AppOption) *App {
	a := &App{
		Registry: NewRegistry(),
		Router:   router.New(),
		MCP:      mcp.NewServer(),
		Config:   AppConfig{JSONCase: CaseCamel},
		Plugins:  NewPluginManager(),
		events:   event.NewEventBus(),
		hooks:    make(map[string]*hook.HookRegistry),
	}

	for _, opt := range opts {
		opt(a)
	}

	// Apply default middleware before any routes are registered. The router
	// wraps handlers at Handle() time, so middleware added after that has
	// no effect — we have to commit to a chain up front. Users who want a
	// custom chain pass WithoutDefaultMiddleware and call App.Use(...) before
	// registering entities.
	a.applyDefaultMiddleware()

	// Propagate DB to registry and its entities
	if a.DB != nil {
		a.Registry.SetDB(a.DB)
	}

	return a
}

// Entity registers an entity with the given name and configuration.
// Returns the App for fluent chaining.
func (a *App) Entity(name string, config entity.EntityConfig) *App {
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

	var crudHandler *CrudHandler
	if crudEnabled {
		crudHandler = NewCrudHandler(e, a.DB)
		crudHandler.JSONCase = a.JSONCasing()
		crudHandler.Hooks = a.HookRegistry(name)
		crudHandler.Storage = a.Storage
		crudHandler.Events = a.Events()
		crudHandler.Registry = a.Registry
		RegisterCrudRoutes(a.Router, crudHandler, "/"+e.GetTable())
	}

	if config.MCP && a.DB != nil {
		if err := RegisterEntityMCPTools(a.MCP, crudHandler, a.Router); err != nil {
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

func (a *App) registerEntityEndpoints(ent *entity.Entity, endpoints []entity.Endpoint) error {
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		path := entityEndpointPath(ent, endpoint.Path)
		if endpoint.Handler != nil {
			a.Router.Handle(method, path, endpoint.Handler)
		}
		if endpoint.MCP {
			if endpoint.MCPHandler == nil {
				return fmt.Errorf("endpoint %q: MCPHandler is required when MCP is true", endpoint.Path)
			}
			name := endpoint.Name
			if name == "" {
				name = defaultEndpointToolName(ent.GetName(), method, path)
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

func entityEndpointPath(ent *entity.Entity, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + strings.Trim(ent.GetTable(), "/") + "/" + strings.TrimPrefix(path, "/")
	}
	return normalizePath(convertColonParams(path))
}

func convertColonParams(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

func defaultEndpointToolName(entityName, method, path string) string {
	cleaned := strings.Trim(path, "/")
	cleaned = strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(cleaned)
	return strings.ToLower(entityName + "_" + method + "_" + cleaned)
}

// JSONCasing returns the configured JSON casing strategy.
// Defaults to CaseCamel if not explicitly set.
func (a *App) JSONCasing() JSONCase {
	if a.Config.JSONCase == "" {
		return CaseCamel
	}
	return a.Config.JSONCase
}

// RegisterPlugin registers a plugin with the application's plugin manager.
// Returns the App for fluent chaining.
func (a *App) RegisterPlugin(plugin Plugin) *App {
	if err := a.Plugins.Register(plugin); err != nil {
		panic(fmt.Sprintf("framework: failed to register plugin %q: %v", plugin.Name(), err))
	}
	return a
}

// InitPlugins initializes all registered plugins and calls their optional
// interface methods (HasRoutes, HasMiddleware, HasTools, HasHooks).
// This should be called after all plugins are registered and before the server starts.
func (a *App) InitPlugins() error {
	if err := a.Plugins.InitAll(a); err != nil {
		return err
	}
	a.Plugins.RegisterRoutes(a.Router)
	a.Plugins.RegisterMiddleware(a)
	a.Plugins.RegisterTools(a.MCP)
	a.Plugins.RegisterHooks(a)
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

// OnStop registers a function to run during App.Stop, after the HTTP
// server has shut down. Hooks run in reverse registration order — the
// last thing started is the first thing stopped.
func (a *App) OnStop(fn func() error) *App {
	a.stopHooks = append(a.stopHooks, fn)
	return a
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

// Stop shuts down the HTTP server (graceful) and runs every OnStop hook in
// reverse order. Safe to call multiple times; subsequent calls are no-ops.
func (a *App) Stop(ctx context.Context) error {
	if a.appCancel != nil {
		a.appCancel()
		a.appCancel = nil
	}
	var firstErr error
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			firstErr = err
		}
		a.server = nil
	}
	// Reverse order — last-started is first-stopped.
	for i := len(a.stopHooks) - 1; i >= 0; i-- {
		if err := a.stopHooks[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// runStartHooks fires every OnStart hook with the app's lifecycle context.
// Returns the first error so Start aborts cleanly before binding the port.
func (a *App) runStartHooks() error {
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}
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
	// Auto-migrate all registered entities
	if a.DB != nil {
		if err := AutoMigrate(a.DB, a.Registry); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
	}

	// Run OnStart hooks (cron/queue workers, custom setup). Failure here
	// aborts before we bind the port — better than a half-up server.
	if err := a.runStartHooks(); err != nil {
		return fmt.Errorf("start hooks: %w", err)
	}

	// Auto-generate and serve OpenAPI spec
	if len(a.Registry.All()) > 0 {
		appName := a.Config.Name
		if appName == "" {
			appName = "GoFastr API"
		}
		spec := EntityOpenAPI(a.Registry, appName, "1.0.0")
		a.Router.Get("/openapi.json", openapi.Handler(spec))
		a.Router.Get("/docs/", openapi.SwaggerUIHandler(spec, "/docs"))
	}

	if a.Config.DebugEndpoints {
		a.registerDebugEndpoints()
	}

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

	// Log OpenAPI
	fmt.Printf("  %s OpenAPI:     http://%s/openapi.json\n", arrow(), host)
	fmt.Printf("  %s Swagger UI:  http://%s/docs/\n\n", arrow(), host)

	a.server = &http.Server{
		Addr:    addr,
		Handler: a.Router,
	}
	return a.server.ListenAndServe()
}

// registerDebugEndpoints adds /.debug/stats for runtime diagnostics.
func (a *App) registerDebugEndpoints() {
	a.Router.Get("/.debug/stats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// Shutdown gracefully stops the HTTP server, waiting up to ctx's deadline for
// in-flight requests to complete. Safe to call before Start (no-op).
func (a *App) Shutdown(ctx context.Context) error {
	if a.server == nil {
		return nil
	}
	return a.server.Shutdown(ctx)
}
