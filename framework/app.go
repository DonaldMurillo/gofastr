package framework

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gofastr/gofastr/core/mcp"
	"github.com/gofastr/gofastr/core/router"
)

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name string // application name
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

	server   *http.Server
	events   *EventBus
	hooks    map[string]*HookRegistry
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
		Config:   AppConfig{},
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
		// Store the error so the caller can check via a Validate() call
		// or we panic for now — fluent APIs shouldn't silently fail
		panic(fmt.Sprintf("framework: failed to register entity %q: %v", name, err))
	}

	return a
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
func (a *App) Start(addr string) error {
	a.server = &http.Server{
		Addr:    addr,
		Handler: a.Router,
	}
	return a.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (a *App) Shutdown(ctx context.Context) error {
	if a.server == nil {
		return nil
	}
	return a.server.Shutdown(ctx)
}
