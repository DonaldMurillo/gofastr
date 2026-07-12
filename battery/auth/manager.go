package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
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
	// In production mode (DevMode=false) it is mandatory: Init fails closed
	// with an error when it is empty, because an empty HMAC key yields
	// forgeable JWTs. In DevMode a random per-process secret is minted.
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

	// UserStore looks up and creates users. Unlike SessionStore, there
	// is NO default: if nil, the register/login/magic-link/OAuth
	// handlers fail with "user store not configured". Wire an
	// auth.EntityUserStore (or your own implementation) in every
	// deployment, including dev/test.
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

	// RegisterRateLimit applies a per-IP rate limit to /auth/register.
	// Defaults to 10/min with a 15-minute block when nil — unthrottled
	// registration is account-table flooding and, once an EmailSender
	// is wired, an email-bombing primitive. To disable, pass a config
	// with a huge MaxAttempts.
	RegisterRateLimit *RateLimiterConfig

	// DevMode loosens the cookie defaults for local HTTP development.
	// When true: SessionSecure=false, SessionCookie="session_id".
	// When false (production default): SessionSecure=true, cookie name
	// "__Host-session" — the prefix forces browsers to reject the cookie
	// unless Path=/, Secure, no Domain, blocking subdomain cookie
	// injection.
	DevMode bool

	// AllowInMemoryStores acknowledges a deliberate single-node
	// deployment. Without it, production mode (DevMode=false) logs a
	// WARN at Init when auth state lives in the default in-memory
	// stores: sessions don't survive restarts and never resolve on a
	// second replica, and in-memory 2FA state silently reverts enrolled
	// accounts to password-only auth after a restart. See the
	// horizontal-scaling doc for the DB-backed alternatives.
	AllowInMemoryStores bool

	// DefaultRoles are the server-assigned roles stamped onto every
	// newly created account — register, magic-link auto-create, and
	// OAuth auto-create all draw from it. When empty (the default),
	// ["user"] is used.
	//
	// These are operator configuration, NEVER request data: the
	// registration and auto-create flows are anonymous, so honoring a
	// client-supplied roles key would let anyone self-promote. The
	// handlers read this value through AuthManager.DefaultRoles() and
	// ignore any roles field on the incoming request.
	DefaultRoles []string

	// AuditSink, when non-nil, receives security events: login success
	// and failure, 2FA enrolment/challenge/disable, password reset,
	// OAuth link and login, magic-link request and consume. The events
	// land in the same audit_log table as the CRUD hooks (via
	// NewSQLAuditSink) so an operator has one trail for "who did what".
	// nil disables auditing entirely — emit calls are no-ops. Wire it
	// with auth.NewSQLAuditSink(db, "") for the one-line default.
	AuditSink AuditSink
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
	// Default brute-force throttles. These run in BOTH DevMode and
	// production because credential-stuffing is a network attack, not
	// a config-mode attack — leaving /auth/login un-throttled in dev
	// has been the source of every "we wrote a test and it found 22
	// vulns" story since 2016. Apps that really want unlimited login
	// throughput can pass a custom config with a huge MaxAttempts.
	if c.LoginRateLimit == nil {
		c.LoginRateLimit = &RateLimiterConfig{
			MaxAttempts:   30, // ~ generous per-IP burst
			Window:        time.Minute,
			BlockDuration: 5 * time.Minute,
		}
	}
	if c.LoginRateLimitPerAccount == nil {
		c.LoginRateLimitPerAccount = &RateLimiterConfig{
			MaxAttempts:   10, // ~ tight per-account budget
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	if c.RegisterRateLimit == nil {
		c.RegisterRateLimit = &RateLimiterConfig{
			MaxAttempts:   10, // account creation is rare; 10/min/IP is generous
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	if c.DevMode {
		if c.SessionCookie == "" {
			c.SessionCookie = "session_id"
		}
		// In dev with no explicit JWTSecret, mint a random per-process
		// secret so the boilerplate doesn't ship a literal "change-me"
		// string. Sessions invalidate on restart — set JWTSecret if you
		// need stable dev tokens across restarts.
		if c.JWTSecret == "" {
			secret, err := randomDevJWTSecret()
			if err == nil {
				c.JWTSecret = secret
				slog.Default().Warn("auth: DevMode minted a random per-process JWTSecret",
					"recommendation", "set AuthConfig.JWTSecret if you need stable dev tokens across restarts")
			}
		}
		// SessionSecure stays false (zero value) in dev.
		return
	}
	// Production defaults: secure-by-default.
	if c.SessionCookie == "" {
		c.SessionCookie = "__Host-session"
	}
	c.SessionSecure = true
	// No signing key in production is a fatal misconfiguration: an empty
	// HMAC secret yields forgeable JWTs and sessions that don't survive
	// a restart. Init fails closed on it (see AuthManager.Init) — DevMode
	// mints a per-process secret; production must supply one explicitly.
}

// randomDevJWTSecret returns 32 cryptographically-random bytes encoded
// as URL-safe base64 — enough entropy for the HMAC-SHA256 signing
// path NewJWTAuth uses. Only called when DevMode is on and no
// JWTSecret was supplied.
func randomDevJWTSecret() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
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

	// UpdateRoles replaces the roles for an existing user. Returns
	// ErrUserNotFound if the user does not exist. The roles slice is
	// OPERATOR input (from an admin screen), never request data — the
	// same security posture as AuthConfig.DefaultRoles.
	UpdateRoles(ctx context.Context, userID string, roles []string) error
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

// DefaultRoles returns the roles assigned to newly created accounts,
// from AuthConfig.DefaultRoles or ["user"] when that field is empty.
// It returns a fresh copy on every call so each caller owns its slice
// and cannot mutate the shared configuration through it.
//
// This is the single source of truth the register handler and the
// OAuth/magic-link auto-create flows read; it is server-side
// configuration and is never influenced by request input.
func (m *AuthManager) DefaultRoles() []string {
	if len(m.config.DefaultRoles) > 0 {
		out := make([]string, len(m.config.DefaultRoles))
		copy(out, m.config.DefaultRoles)
		return out
	}
	return []string{"user"}
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

// SetUserRoles replaces the roles for an existing user via the configured
// UserStore. It is the supported server-side entry point for operator-driven
// role assignment (e.g. the admin back-office). There is no HTTP route —
// call it from trusted server code. The roles are OPERATOR input, never
// request data: the caller is responsible for sourcing them from an
// admin-gated screen, not from a client-supplied body.
func (m *AuthManager) SetUserRoles(ctx context.Context, userID string, roles []string) error {
	return m.userStore.UpdateRoles(ctx, userID, roles)
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
// Init fails closed when DevMode=false and JWTSecret is empty: the app
// refuses to start rather than run with a forgeable signing key.
//
// app may be nil for unit tests that exercise auth in isolation; in
// that case route mounting is skipped (the test wires routes directly
// onto a router it owns).
func (m *AuthManager) Init(app *framework.App) error {
	// Fail closed in production: an empty JWTSecret with DevMode=false
	// yields forgeable, restart-unstable JWTs. Refuse to boot rather than
	// warn — DevMode mints its own per-process secret, so it's exempt.
	if !m.config.DevMode && m.config.JWTSecret == "" {
		return fmt.Errorf("auth: production mode requires AuthConfig.JWTSecret — set it from your secret store, or set DevMode: true for local development")
	}

	// Single-node state in production is the silent multi-replica
	// failure: the second replica never resolves the first one's
	// cookies. Warn loudly unless the host explicitly opted in.
	if !m.config.DevMode && !m.config.AllowInMemoryStores {
		if _, ok := m.sessionStore.(*MemorySessionStore); ok {
			slog.Default().Warn("auth: production mode is running on the in-memory session store — sessions won't survive a restart and won't resolve on a second replica",
				"fix", "use NewEntitySessionStore(db, ...) (or another durable SessionStore)",
				"single-node opt-in", "set AuthConfig.AllowInMemoryStores: true to acknowledge and silence this")
		}
	}

	// Initialize JWT if secret is configured
	if m.config.JWTSecret != "" {
		expiry := m.config.JWTExpiry
		if expiry == 0 {
			expiry = time.Hour
		}
		m.jwtAuth = NewJWTAuth(m.config.JWTSecret, expiry)
	}

	// The auth battery owns its tables: create them if absent so hosts never
	// hand-roll the auth_users / auth_sessions DDL. Stores that don't manage a
	// schema (custom backends) simply don't implement the optional interface.
	ctx := context.Background()
	for _, st := range []any{m.userStore, m.sessionStore} {
		if se, ok := st.(interface {
			EnsureSchema(context.Context) error
		}); ok {
			if err := se.EnsureSchema(ctx); err != nil {
				return fmt.Errorf("auth: ensure schema: %w", err)
			}
		}
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
