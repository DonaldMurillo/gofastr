package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/gofastr/gofastr/core/openapi"

	"github.com/gofastr/gofastr/core/mcp"
	"github.com/gofastr/gofastr/core/router"
)

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
	Name     string   // application name
	JSONCase JSONCase // JSON key casing: "camelCase" (default) or "snake_case"
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

	server *http.Server
	events *EventBus
	hooks  map[string]*HookRegistry
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

// NewApp creates a new App with the given options.
// It initializes default Registry, Router, and MCP Server if not provided.
func NewApp(opts ...AppOption) *App {
	a := &App{
		Registry: NewRegistry(),
		Router:   router.New(),
		MCP:      mcp.NewServer(),
		Config:   AppConfig{JSONCase: CaseCamel},
		Plugins:  NewPluginManager(),
		events:   NewEventBus(),
		hooks:    make(map[string]*HookRegistry),
	}

	for _, opt := range opts {
		opt(a)
	}

	// Propagate DB to registry and its entities
	if a.DB != nil {
		a.Registry.SetDB(a.DB)
	}

	return a
}

// Entity registers an entity with the given name and configuration.
// Returns the App for fluent chaining.
func (a *App) Entity(name string, config EntityConfig) *App {
	e := Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	if err := a.Registry.Register(e); err != nil {
		panic(fmt.Sprintf("framework: failed to register entity %q: %v", name, err))
	}

	// Auto-register CRUD routes.
	// Default (CRUD==nil): auto-register when DB is set.
	// Set CRUD to &true to always register, &false to opt out.
	if config.CRUD != nil {
		if *config.CRUD && a.DB != nil {
			handler := NewCrudHandler(e, a.DB)
			handler.JSONCase = a.JSONCasing()
			RegisterCrudRoutes(a.Router, handler, "/"+e.GetTable())
		}
	} else if a.DB != nil {
		handler := NewCrudHandler(e, a.DB)
		handler.JSONCase = a.JSONCasing()
		RegisterCrudRoutes(a.Router, handler, "/"+e.GetTable())
	}

	return a
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
func (a *App) Events() *EventBus {
	if a.events == nil {
		a.events = NewEventBus()
	}
	return a.events
}

// HookRegistry returns (or creates) the hook registry for a named entity.
func (a *App) HookRegistry(entityName string) *HookRegistry {
	if a.hooks == nil {
		a.hooks = make(map[string]*HookRegistry)
	}
	if _, ok := a.hooks[entityName]; !ok {
		a.hooks[entityName] = NewHookRegistry()
	}
	return a.hooks[entityName]
}

// Start starts the HTTP server on the given address.
// Auto-migrates tables, registers OpenAPI/Swagger, debug stats.
// Sets the process title to the app name for visibility in ps/Activity Monitor.
func (a *App) Start(addr string) error {
	// Auto-migrate all registered entities
	if a.DB != nil {
		if err := AutoMigrate(a.DB, a.Registry); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
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

	a.registerDebugEndpoints()

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
	fmt.Printf("  %s Stats: http://%s/.debug/stats\n", arrow(), host)

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

// Shutdown gracefully shuts down the HTTP server.
func (a *App) Shutdown(ctx context.Context) error {
	if a.server == nil {
		return nil
	}
	return a.server.Shutdown(ctx)
}
