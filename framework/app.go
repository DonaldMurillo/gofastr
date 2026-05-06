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

	server *http.Server
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
