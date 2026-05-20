package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
)

// AuthConfig is the top-level configuration for the auth battery.
// It is passed to New() and stored on AuthManager.
type AuthConfig struct {
	// JWTSecret is the signing key for JWT tokens. Required for JWT-based auth.
	JWTSecret string

	// JWTExpiry is the duration for which JWT tokens are valid.
	// Defaults to 1 hour if zero.
	JWTExpiry time.Duration

	// SessionCookie is the cookie name for session-based auth.
	// Defaults to "session_id".
	SessionCookie string

	// SessionTTL is how long a session lives. Defaults to 7 days.
	SessionTTL time.Duration

	// SessionSecure sets the Secure flag on session cookies.
	// Should be true in production (HTTPS).
	SessionSecure bool

	// BasePath is the URL prefix for all auth routes. Defaults to "/auth".
	BasePath string

	// UserStore is the interface for looking up users by email/id.
	// If nil, an in-memory store is used (dev/test only).
	UserStore UserStore

	// SessionStore is the storage backend for sessions.
	// If nil, an in-memory store is used (dev/test only).
	SessionStore SessionStore

	// LoginRateLimit, when non-nil, applies a per-IP rate limit to
	// /auth/login. nil means unlimited (which you almost never want
	// in production — set defaults if unsure).
	LoginRateLimit *RateLimiterConfig

	// LoginRateLimitPerAccount, when non-nil, applies a per-account
	// (keyed on the submitted email, lowercased) rate limit to
	// /auth/login. Combined with the per-IP limiter it defeats the
	// attacker who pivots through a botnet's worth of IPs to pound a
	// single account. nil disables the per-account limit.
	LoginRateLimitPerAccount *RateLimiterConfig

	// DevMode loosens the cookie defaults for local HTTP development.
	// When true: SessionSecure=false, SessionCookie="session_id".
	// When false (production default): SessionSecure=true, cookie name
	// "__Host-session" — the prefix forces browsers to reject the cookie
	// unless Path=/, Secure, no Domain, blocking subdomain cookie
	// injection.
	DevMode bool
}

// defaults fills in zero values with sensible defaults.
//
// Production defaults (DevMode=false):
//   - SessionCookie = "__Host-session" (browser-enforced subdomain
//     isolation; requires Path=/, Secure, no Domain).
//   - SessionSecure = true.
//
// Dev defaults (DevMode=true):
//   - SessionCookie = "session_id".
//   - SessionSecure = false.
func (c *AuthConfig) defaults() {
	if c.BasePath == "" {
		c.BasePath = "/auth"
	}
	if c.SessionTTL <= 0 {
		// 7 days matches MemorySessionStore.Create's previous fallback.
		// Without this, EntitySessionStore.Create would silently mint
		// already-expired sessions (broken login on the very next request).
		c.SessionTTL = 7 * 24 * time.Hour
	}
	if c.DevMode {
		if c.SessionCookie == "" {
			c.SessionCookie = "session_id"
		}
		// SessionSecure stays false (zero value) in dev.
		return
	}
	// Production defaults: secure-by-default.
	if c.SessionCookie == "" {
		c.SessionCookie = "__Host-session"
	}
	c.SessionSecure = true
}

// UserStore is the interface auth needs to look up and manage users.
// This decouples auth from any specific storage backend.
type UserStore interface {
	// FindByEmail looks up a user by email. Returns the user and their
	// hashed password, or ErrUserNotFound if no row matches. Other errors
	// (e.g. transport failures) are returned verbatim so handlers can
	// distinguish "user doesn't exist" from "DB unavailable".
	FindByEmail(ctx context.Context, email string) (User, string, error)

	// FindByID looks up a user by their unique ID. Returns ErrUserNotFound
	// if no row matches.
	FindByID(ctx context.Context, id string) (User, error)

	// CreateUser creates a new user account. Returns ErrEmailTaken if the
	// email is already registered.
	CreateUser(ctx context.Context, email, hashedPassword string, roles []string) (User, error)
}

// AuthPlugin is the interface for composable auth features. Each plugin
// adds authentication capabilities (email/password, OAuth2, 2FA, etc.)
// to the AuthManager.
//
// Plugins are registered via AuthManager.Use() and initialized in order
// during AuthManager.Init().
type AuthPlugin interface {
	// Name returns the unique plugin identifier.
	Name() string

	// Init initializes the plugin with access to the AuthManager.
	// The manager is fully configured (stores, config) but not yet started.
	Init(mgr *AuthManager) error
}

// AuthPluginRoutes is the optional interface for plugins that register
// HTTP routes under the auth base path.
type AuthPluginRoutes interface {
	AuthPlugin
	RegisterRoutes(r *router.Router, basePath string)
}

// AuthPluginMiddleware is the optional interface for plugins that provide
// middleware (e.g., session hydration, 2FA challenge).
type AuthPluginMiddleware interface {
	AuthPlugin
	Middleware() func(http.Handler) http.Handler
}

// AuthPluginOnStart is the optional interface for plugins that need
// startup logic (e.g., OAuth2 provider validation, cleanup goroutines).
type AuthPluginOnStart interface {
	AuthPlugin
	OnStart(ctx context.Context) error
}

// AuthPluginOnStop is the optional interface for plugins that need
// shutdown logic (e.g., cleanup goroutines).
type AuthPluginOnStop interface {
	AuthPlugin
	OnStop(ctx context.Context) error
}

// AuthManager is the core orchestrator for the auth system. It manages
// the plugin lifecycle, provides shared state (stores, config), and
// implements the framework.Battery interface so it can be registered
// directly on an App.
//
// Usage:
//
//	mgr := auth.New(auth.Config{
//	    JWTSecret: "secret",
//	})
//	mgr.Use(oauth2plugin.New(oauth2plugin.Config{...}))
//	mgr.Use(twofaplugin.New())
//	app.RegisterBattery(mgr)
type AuthManager struct {
	config  AuthConfig
	plugins map[string]AuthPlugin
	order   []string

	// Shared state — available to all plugins via AuthManager accessors.
	userStore    UserStore
	sessionStore SessionStore
	jwtAuth      *JWTAuth

	mu      sync.RWMutex
	started bool
}

// New creates a new AuthManager with the given configuration.
// The core auth plugin (email/password + sessions + JWT) is always loaded first.
func New(config AuthConfig) *AuthManager {
	config.defaults()
	mgr := &AuthManager{
		config:  config,
		plugins: make(map[string]AuthPlugin),
	}

	// Apply user/session store from config or defaults
	if config.UserStore != nil {
		mgr.userStore = config.UserStore
	}
	if config.SessionStore != nil {
		mgr.sessionStore = config.SessionStore
	} else {
		mgr.sessionStore = NewMemorySessionStore()
	}

	return mgr
}

// Use registers an auth plugin. Returns the manager for chaining.
// Plugins are initialized in registration order during Init().
func (m *AuthManager) Use(plugin AuthPlugin) *AuthManager {
	if _, exists := m.plugins[plugin.Name()]; exists {
		panic(fmt.Sprintf("auth: plugin %q already registered", plugin.Name()))
	}
	m.plugins[plugin.Name()] = plugin
	m.order = append(m.order, plugin.Name())
	return m
}

// Plugin retrieves a registered plugin by name. Useful for cross-plugin
// communication (e.g., OAuth2 plugin checking if 2FA plugin is active).
func (m *AuthManager) Plugin(name string) (AuthPlugin, bool) {
	p, ok := m.plugins[name]
	return p, ok
}

// PluginAs retrieves a plugin and type-asserts it to T.
func PluginAs[T AuthPlugin](m *AuthManager, name string) (T, error) {
	var zero T
	p, ok := m.plugins[name]
	if !ok {
		return zero, fmt.Errorf("auth: plugin %q not found", name)
	}
	typed, ok := p.(T)
	if !ok {
		return zero, fmt.Errorf("auth: plugin %q is not %T", name, zero)
	}
	return typed, nil
}

// Config returns the auth configuration (read-only copy).
func (m *AuthManager) Config() AuthConfig {
	return m.config
}

// SessionStore returns the session storage backend.
func (m *AuthManager) SessionStore() SessionStore {
	return m.sessionStore
}

// UserStore returns the user storage backend.
func (m *AuthManager) UserStore() UserStore {
	return m.userStore
}

// JWT returns the JWT auth helper (nil if JWT is not configured).
func (m *AuthManager) JWT() *JWTAuth {
	return m.jwtAuth
}

// SetUserStore allows plugins or external code to set the user store
// after construction but before Init.
func (m *AuthManager) SetUserStore(store UserStore) {
	m.userStore = store
}

// SetSessionStore allows plugins or external code to set the session store.
func (m *AuthManager) SetSessionStore(store SessionStore) {
	m.sessionStore = store
}

// initPlugins initializes all registered plugins in order.
func (m *AuthManager) initPlugins() error {
	for _, name := range m.order {
		p := m.plugins[name]
		if err := p.Init(m); err != nil {
			return fmt.Errorf("auth plugin %q init failed: %w", name, err)
		}
	}
	return nil
}

// ─── framework.Battery interface ────────────────────────────────────────

// Name returns the battery name for the framework's BatteryManager.
func (m *AuthManager) Name() string { return "auth" }

// Init is called by the framework during App.Start. It initializes JWT
// (if configured), all registered auth plugins, and mounts their HTTP
// routes on app.Router() under the configured BasePath.
//
// app may be nil for unit tests that exercise auth in isolation; in
// that case route mounting is skipped (the test wires routes directly
// onto a router it owns).
func (m *AuthManager) Init(app *framework.App) error {
	// Initialize JWT if secret is configured
	if m.config.JWTSecret != "" {
		expiry := m.config.JWTExpiry
		if expiry == 0 {
			expiry = time.Hour
		}
		m.jwtAuth = NewJWTAuth(m.config.JWTSecret, expiry)
	}

	// Init all plugins
	if err := m.initPlugins(); err != nil {
		return err
	}

	if app != nil {
		m.RegisterRoutes(app.Router())
	}

	return nil
}

// OnStart starts plugins that implement AuthPluginOnStart.
func (m *AuthManager) OnStart(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	for _, name := range m.order {
		if sp, ok := m.plugins[name].(AuthPluginOnStart); ok {
			if err := sp.OnStart(ctx); err != nil {
				return fmt.Errorf("auth plugin %q start failed: %w", name, err)
			}
		}
	}
	return nil
}

// OnStop stops plugins that implement AuthPluginOnStop.
func (m *AuthManager) OnStop(ctx context.Context) error {
	var firstErr error
	for i := len(m.order) - 1; i >= 0; i-- {
		name := m.order[i]
		if sp, ok := m.plugins[name].(AuthPluginOnStop); ok {
			if err := sp.OnStop(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("auth plugin %q stop failed: %w", name, err)
			}
		}
	}
	return firstErr
}

// RegisterRoutes mounts all sub-plugin routes under the configured
// auth base path. Called from AuthManager.Init when an app is supplied;
// also exported so users can mount auth routes onto a router they
// manage themselves.
func (m *AuthManager) RegisterRoutes(r *router.Router) {
	for _, name := range m.order {
		if rp, ok := m.plugins[name].(AuthPluginRoutes); ok {
			rp.RegisterRoutes(r, m.config.BasePath)
		}
	}
}

// Middleware returns a composed middleware chain from all plugins that
// implement AuthPluginMiddleware. Middleware is applied in registration order.
func (m *AuthManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Apply plugin middleware in reverse order so the first registered
		// plugin is the outermost middleware.
		handler := next
		for i := len(m.order) - 1; i >= 0; i-- {
			if pmw, ok := m.plugins[m.order[i]].(AuthPluginMiddleware); ok {
				handler = pmw.Middleware()(handler)
			}
		}
		return handler
	}
}
