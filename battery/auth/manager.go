package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
)

// AuthConfig is the top-level configuration for the auth battery.
type AuthConfig struct {
	// JWTSecret is the JWT signing key; Init requires it outside DevMode.
	JWTSecret string
	// JWTPreviousSecrets are verify-only keys kept for a rotation's drain window.
	JWTPreviousSecrets []string

	// JWTExpiry is a JWT's lifetime; zero means one hour.
	JWTExpiry time.Duration

	// CookieOnly leaves JWTs out of login responses; WithBFFPosture sets it.
	CookieOnly bool

	// SessionCookie is the cookie name for session-based auth.
	SessionCookie string

	// SessionTTL is a session's lifetime; zero means seven days.
	SessionTTL time.Duration

	// SessionSecure sets the Secure flag on session cookies.
	SessionSecure bool

	// BasePath is the URL prefix for all auth routes. Defaults to "/auth".
	BasePath string

	// UserStore looks up and creates users; it has no default.
	UserStore UserStore

	// SessionStore stores sessions; nil means an in-memory store.
	SessionStore SessionStore

	// GlobalSessionLookup declares that session and user lookups ignore the context tenant.
	GlobalSessionLookup bool

	// LoginRateLimit is the per-IP limit on /auth/login; nil means the default.
	LoginRateLimit *RateLimiterConfig

	// LoginRateLimitPerAccount is the per-address limit on /auth/login; nil means the default.
	LoginRateLimitPerAccount *RateLimiterConfig

	// RegisterRateLimit applies a per-IP rate limit to /auth/register.
	RegisterRateLimit *RateLimiterConfig

	// MailAddressRateLimit is the per-address mail budget magic-link send and forgot-password share; nil means the default.
	MailAddressRateLimit *RateLimiterConfig

	// DevMode loosens the cookie defaults for local HTTP development.
	DevMode bool

	// AllowInMemoryStores lets production boot on the in-memory session and 2FA stores.
	AllowInMemoryStores bool

	// DefaultRoles are the roles every new account gets; empty means ["user"].
	DefaultRoles []string

	// AuditSink receives security events; nil disables them.
	AuditSink AuditSink

	// RegisterEmailSender mails the existing holder when someone registers their address; nil sends nothing.
	RegisterEmailSender EmailSender

	// CanonicalizeEmail replaces CanonicalEmail at every ingestion point; it must be idempotent.
	CanonicalizeEmail func(email string) (string, error)

	// Pages renders the battery's own screens; nil keeps the built-in documents.
	Pages Pages
}

func (c *AuthConfig) defaults() {
	if c.BasePath == "" {
		c.BasePath = "/auth"
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = 7 * 24 * time.Hour
	}
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
	if c.MailAddressRateLimit == nil {
		c.MailAddressRateLimit = defaultMailAddressRateLimit()
	}
	if c.DevMode {
		if c.SessionCookie == "" {
			c.SessionCookie = "session_id"
		}
		if c.JWTSecret == "" {
			secret, err := randomDevJWTSecret()
			if err == nil {
				c.JWTSecret = secret
				slog.Default().Warn("auth: DevMode minted a random per-process JWTSecret",
					"recommendation", "set AuthConfig.JWTSecret if you need stable dev tokens across restarts")
			}
		}
		return
	}
	if c.SessionCookie == "" {
		c.SessionCookie = "__Host-session"
	}
	c.SessionSecure = true
}

func randomDevJWTSecret() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func defaultMailAddressRateLimit() *RateLimiterConfig {
	return &RateLimiterConfig{
		MaxAttempts:   1,
		Window:        2 * time.Minute,
		BlockDuration: 2 * time.Minute,
	}
}

// UserStore is the interface auth needs to look up and manage users.
type UserStore interface {
	// FindByEmail returns the user and password hash, or ErrUserNotFound.
	FindByEmail(ctx context.Context, email string) (User, string, error)

	// FindByID returns the user, or ErrUserNotFound.
	FindByID(ctx context.Context, id string) (User, error)

	// CreateUser creates an account, or returns ErrEmailTaken.
	CreateUser(ctx context.Context, email, hashedPassword string, roles []string) (User, error)

	// UpdateRoles replaces a user's roles, or returns ErrUserNotFound.
	UpdateRoles(ctx context.Context, userID string, roles []string) error
}

// AuthPlugin is one composable auth feature registered with AuthManager.Use.
type AuthPlugin interface {
	// Name returns the unique plugin identifier.
	Name() string

	// Init initializes the plugin with access to the AuthManager.
	Init(mgr *AuthManager) error
}

// AuthPluginRoutes is a plugin that mounts routes under the base path.
type AuthPluginRoutes interface {
	AuthPlugin
	RegisterRoutes(r *router.Router, basePath string)
}

// AuthPluginMiddleware is a plugin that contributes middleware.
type AuthPluginMiddleware interface {
	AuthPlugin
	Middleware() func(http.Handler) http.Handler
}

// AuthPluginOnStart is a plugin with startup work.
type AuthPluginOnStart interface {
	AuthPlugin
	OnStart(ctx context.Context) error
}

// AuthPluginOnStop is a plugin with shutdown work.
type AuthPluginOnStop interface {
	AuthPlugin
	OnStop(ctx context.Context) error
}

// AuthManager runs the auth plugins over shared stores and is a framework.Battery.
type AuthManager struct {
	config  AuthConfig
	plugins map[string]AuthPlugin
	order   []string

	userStore    UserStore
	sessionStore SessionStore
	jwtAuth      *JWTAuth
	mailAddressLimit *RateLimiter

	mu      sync.RWMutex
	started bool

	app       *framework.App
	resolving bool

	unregisterMemoryErase func()
	memoryEraseName       string
}

func (m *AuthManager) noteIfWired() {
	m.mu.RLock()
	app, ok := m.app, m.resolving
	m.mu.RUnlock()
	if app != nil && ok {
		app.NoteAuthenticator()
	}
}

func (m *AuthManager) markResolving() {
	m.mu.Lock()
	m.resolving = true
	m.mu.Unlock()
	m.noteIfWired()
}

// New creates a new AuthManager with the given configuration.
func New(config AuthConfig) *AuthManager {
	config.defaults()
	mgr := &AuthManager{
		config:  config,
		plugins: make(map[string]AuthPlugin),
	}

	if config.UserStore != nil {
		mgr.userStore = config.UserStore
	}
	if config.SessionStore != nil {
		mgr.sessionStore = config.SessionStore
	} else {
		mgr.sessionStore = NewMemorySessionStore()
	}
	mgr.mailAddressLimit = newScopedRateLimiter(*config.MailAddressRateLimit, "mail_address")
	if config.JWTSecret != "" {
		mgr.jwtAuth = NewJWTAuth(config.JWTSecret, config.JWTExpiry)
		mgr.jwtAuth.onResolve = mgr.markResolving
	}

	return mgr
}

// Use registers an auth plugin. Returns the manager for chaining.
func (m *AuthManager) Use(plugin AuthPlugin) *AuthManager {
	if _, exists := m.plugins[plugin.Name()]; exists {
		panic(fmt.Sprintf("auth: plugin %q already registered", plugin.Name()))
	}
	m.plugins[plugin.Name()] = plugin
	m.order = append(m.order, plugin.Name())
	return m
}

// Plugin returns a registered plugin by name.
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

func (m *AuthManager) allowMailAddress(ctx context.Context, email string) bool {
	return allowAddress(ctx, m.mailAddressLimit, email)
}

func (m *AuthManager) canonicalizeEmail(email string) (string, error) {
	return canonicalizeEmailWith(m.config.CanonicalizeEmail, email)
}

// Config returns the auth configuration (read-only copy).
func (m *AuthManager) Config() AuthConfig {
	return m.config
}

// DefaultRoles returns a copy of the roles new accounts get.
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

func (m *AuthManager) globalSessionLookup() bool {
	if m.config.GlobalSessionLookup {
		return true
	}
	if _, builtIn := m.UserStore().(*EntityUserStore); !builtIn {
		return false
	}
	switch m.SessionStore().(type) {
	case *MemorySessionStore, *EntitySessionStore:
		return true
	default:
		return false
	}
}

// UserStore returns the user storage backend.
func (m *AuthManager) UserStore() UserStore {
	return m.userStore
}

// JWT returns the JWT auth helper (nil if JWT is not configured).
func (m *AuthManager) JWT() *JWTAuth {
	return m.jwtAuth
}

// SetUserStore replaces the user store; call it before Init.
func (m *AuthManager) SetUserStore(store UserStore) {
	m.userStore = store
}

// SetSessionStore allows plugins or external code to set the session store.
func (m *AuthManager) SetSessionStore(store SessionStore) {
	m.sessionStore = store
}

// SetUserRoles replaces a user's roles from trusted server code and audits roles.updated.
func (m *AuthManager) SetUserRoles(ctx context.Context, userID string, roles []string) error {
	if !textsafe.Storable(userID) {
		return ErrUserNotFound
	}
	if err := m.userStore.UpdateRoles(ctx, userID, roles); err != nil {
		return err
	}
	m.emitSecurity(ctx, SecurityEvent{
		Kind:   "roles.updated",
		UserID: userID,
		Meta:   map[string]string{"roles": strings.Join(roles, ",")},
	})
	return nil
}

func (m *AuthManager) refuseInMemorySessionStore() error {
	if m.config.DevMode || m.config.AllowInMemoryStores {
		return nil
	}
	if _, ok := m.sessionStore.(*MemorySessionStore); ok {
		return fmt.Errorf("auth: production mode refuses to boot on the default in-memory session store: sessions won't survive a restart and won't resolve on a second replica; set AuthConfig.AllowInMemoryStores: true to acknowledge a single-node deployment, or use NewEntitySessionStore(db, ...) (or another durable SessionStore)")
	}
	return nil
}

func (m *AuthManager) initPlugins() error {
	for _, name := range m.order {
		p := m.plugins[name]
		if err := p.Init(m); err != nil {
			return fmt.Errorf("auth plugin %q init failed: %w", name, err)
		}
	}
	return nil
}


// Name returns the battery name for the framework's BatteryManager.
func (m *AuthManager) Name() string { return "auth" }

// ReservedEmbedPrefixes reports BasePath so no embed grant reaches the auth routes.
func (m *AuthManager) ReservedEmbedPrefixes() []string {
	return []string{m.Config().BasePath}
}

// Init initializes JWT and every plugin and, with an app, mounts their routes.
func (m *AuthManager) Init(app *framework.App) error {
	if !m.config.DevMode && m.config.JWTSecret == "" {
		return fmt.Errorf("auth: production mode requires AuthConfig.JWTSecret: set it from your secret store, or set DevMode: true for local development")
	}
	if m.config.SessionTTL < 0 {
		return fmt.Errorf("auth: AuthConfig.SessionTTL is negative (%v): set a positive duration, or leave it zero for the 7-day default", m.config.SessionTTL)
	}

	if err := m.refuseInMemorySessionStore(); err != nil {
		return err
	}

	if m.config.JWTExpiry < 0 {
		return fmt.Errorf("auth: AuthConfig.JWTExpiry is negative (%v): set a positive duration, or leave it zero for the 1-hour default", m.config.JWTExpiry)
	}

	if m.config.JWTSecret != "" {
		expiry := m.config.JWTExpiry
		if expiry == 0 {
			expiry = time.Hour
		}
		if m.jwtAuth == nil {
			m.jwtAuth = NewJWTAuth(m.config.JWTSecret, expiry)
			m.jwtAuth.onResolve = m.markResolving
		}
		m.jwtAuth.Secret = m.config.JWTSecret
		m.jwtAuth.Expiry = expiry
		m.jwtAuth.PreviousSecrets = m.config.JWTPreviousSecrets
		m.jwtAuth.SetUserStore(m.userStore)
	}
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

	if err := m.initPlugins(); err != nil {
		return err
	}

	if err := m.refuseInMemorySessionStore(); err != nil {
		return err
	}

	if m.jwtAuth != nil {
		m.jwtAuth.SetUserStore(m.userStore)
	}

	resolveEraseTables(m)
	resolveExportTables(m)

	if app != nil {
		m.RegisterRoutes(app.Router())
		m.mu.Lock()
		m.app = app
		m.mu.Unlock()
		m.noteIfWired()
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
				return err
			}
		}
	}
	return nil
}

// OnStop stops plugins that implement AuthPluginOnStop.
func (m *AuthManager) OnStop(ctx context.Context) error {
	unregisterMemoryStoreEraser(m)
	var firstErr error
	for _, name := range slices.Backward(m.order) {

		if sp, ok := m.plugins[name].(AuthPluginOnStop); ok {
			if err := sp.OnStop(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("auth plugin %q stop failed: %w", name, err)
			}
		}
	}
	return firstErr
}

// RegisterRoutes mounts every plugin's routes under BasePath.
func (m *AuthManager) RegisterRoutes(r *router.Router) {
	for _, name := range m.order {
		if rp, ok := m.plugins[name].(AuthPluginRoutes); ok {
			rp.RegisterRoutes(r, m.config.BasePath)
		}
	}
}

// Middleware composes the plugins' middleware, first registered outermost.
func (m *AuthManager) Middleware() func(http.Handler) http.Handler {
	m.markResolving()
	return func(next http.Handler) http.Handler {
		handler := next
		for _, v := range slices.Backward(m.order) {
			if pmw, ok := m.plugins[v].(AuthPluginMiddleware); ok {
				handler = pmw.Middleware()(handler)
			}
		}
		return handler
	}
}
