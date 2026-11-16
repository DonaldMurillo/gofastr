package framework

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"path"
	"reflect"
	"runtime"
	"runtime/pprof"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/textsafe"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/core/webbotauth"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/cron"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/outbox"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
	"github.com/DonaldMurillo/gofastr/internal/retired"
)

// Mountable is anything that registers routes on the router; App.Mount attaches it.
type Mountable interface {
	Mount(*router.Router)
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name                  string        // application name
	JSONCase              crud.JSONCase // JSON key casing: "camelCase" (default) or "snake_case"
	DebugEndpoints        bool          // opt-in for /.debug/* endpoints
	NoLLMMD               bool          // disable auto-generated /llm.md entity docs
	PublicOpenAPI         bool          // serve /openapi.json and /api/docs/ without the auth gate
	APIPrefix             string        // mount auto-CRUD entity routes under this path, e.g. "/api"
	BaseURL               string        // origin of the well-known documents; empty derives it from the request
	TrustedProxies        []string      // peers (IPs, CIDRs) whose X-Forwarded-Proto is honored
	RequestTimeout        time.Duration // request budget of the default chain; zero means 30s
	DisableRequestTimeout bool          // drop the timeout middleware and the unset read/write deadlines
	Compression           bool          // gzip text responses in the default chain
	HTTPServerTimeouts    HTTPServerTimeoutsConfig
	TLS                   *TLSConfig                       // nil serves plain HTTP
	HTTPProtocols         *http.Protocols                  // nil keeps net/http's default
	SecurityHeaders       middleware.SecurityHeadersConfig // unset fields keep the strict defaults
	ShutdownTimeout       time.Duration                    // drain budget of the signal handler; zero means 15s
	DisableSignalHandling bool                             // the host owns SIGINT/SIGTERM and calls Shutdown itself
}

const defaultShutdownTimeout = 15 * time.Second

// HTTPServerTimeoutsConfig overrides http.Server deadlines: a nil field keeps its default (read-header
// 10s, read 60s, write 60s, idle 120s) and a pointer to 0 disables that deadline.
type HTTPServerTimeoutsConfig struct {
	ReadHeaderTimeout *time.Duration
	ReadTimeout       *time.Duration
	WriteTimeout      *time.Duration
	IdleTimeout       *time.Duration
}

const (
	defaultServerReadHeaderTimeout = 10 * time.Second
	defaultServerReadTimeout       = 60 * time.Second
	defaultServerWriteTimeout      = 60 * time.Second
	defaultServerIdleTimeout       = 120 * time.Second

	requestTimeoutWriteSlack = 10 * time.Second

	idempotencyInFlightSlack = 10 * time.Second

	defaultRequestTimeout = 30 * time.Second
)

func (a *App) checkIdempotencyInFlightTTL() error {
	if a.idempotency == nil || a.idempotency.Store == nil {
		return nil
	}
	store, reports := a.idempotency.Store.(middleware.InFlightTTLStore)
	budget := a.requestBudget()
	if !reports {
		a.Logger().Warn("framework: the idempotency store does not report its in-flight TTL " +
			"(implement middleware.InFlightTTLStore): a handler that outlives the claim may be executed again by a retry")
		return nil
	}
	if budget <= 0 {
		if a.noDefaults || a.Config.DisableRequestTimeout {
			a.Logger().Warn(fmt.Sprintf("framework: no request deadline is configured (DisableRequestTimeout), so the "+
				"idempotency store's in-flight TTL (%s) is not checked: a handler that runs longer than the claim "+
				"may be executed again by a retry", store.InFlightTTL()))
		}
		return nil
	}
	if ttl := store.InFlightTTL(); ttl <= budget {
		return fmt.Errorf("framework: the idempotency store's in-flight TTL (%s) must be longer than RequestTimeout (%s): "+
			"a claim that expires while the handler still runs lets a retry execute it again; raise the store's "+
			"in-flight TTL (WithMemoryIdempotencyInFlightTTL / WithSQLIdempotencyInFlightTTL) or lower RequestTimeout",
			ttl, budget)
	}
	return nil
}

func (a *App) requestBudget() time.Duration {
	if a.noDefaults || a.Config.DisableRequestTimeout {
		return 0
	}
	if a.Config.RequestTimeout > 0 {
		return a.Config.RequestTimeout
	}
	return defaultRequestTimeout
}

func (a *App) checkRequestWriteTimeouts() error {
	budget := a.requestBudget()
	w := a.Config.HTTPServerTimeouts.WriteTimeout
	if budget == 0 || w == nil || *w <= 0 || *w > budget {
		return nil
	}
	return fmt.Errorf("framework: HTTPServerTimeouts.WriteTimeout (%s) must be longer than RequestTimeout (%s): "+
		"a response finished after the write deadline is never delivered; raise WriteTimeout above RequestTimeout, "+
		"set it to 0, or lower RequestTimeout", *w, budget)
}

// App is the top-level application container.
type App struct {
	Registry            *Registry
	router              *router.Router
	MCP                 *mcp.Server
	DB                  *sql.DB
	Config              AppConfig
	Plugins             *PluginManager
	Storage             upload.Storage // optional; enables multipart on Image/File fields
	imageDeriver        file.ImageDeriver
	fieldImageDerivers  map[string]map[string]file.ImageDeriver // entity -> field -> override
	stripUploadMetadata bool

	auditTable       string
	auditActor       func(context.Context) string
	auditCfg         *AuditConfig
	entityUI         *entityui.UI
	pendingEndpoints map[string][]entity.Endpoint
	claimedEndpoints map[string]bool
	crudMounts       map[*entity.Entity]crudMount
	trustedProxies   clientip.Proxies

	migrationRoutines []migrate.Routine
	migrationViews    []migrate.View

	Batteries *BatteryManager

	embedHost *fembed.Host

	modules *ModuleManager

	access *access.GrantStore

	processModules *ProcessModuleSupervisor

	sandbox processModuleSandbox

	moduleTools *ModuleToolRegistry

	serverMu            sync.Mutex // guards server, appCtx, appCancel and the fields marked serverMu
	server              *http.Server
	boundHost           string // serverMu
	boundInProcess      bool   // serverMu
	shutdownMu          sync.Mutex
	shutdownDone        chan struct{} // shutdownMu
	runHost             Host          // runHost through servicesReadyClosed: serverMu
	booting             bool
	deferredStarting    bool
	stopRequested       bool
	teardownToEngine    bool
	handoffCtx          context.Context
	handoffErr          error
	servicesReady       chan struct{}
	servicesReadyClosed bool
	draining            atomic.Bool
	newConnsMu          sync.Mutex
	newConns            map[net.Conn]struct{}
	streams             openStreams
	events              *event.EventBus
	hooksMu             sync.RWMutex // guards hooks: kiln's build mode registers while the app serves
	hooks               map[string]*hook.HookRegistry
	mountables          []Mountable
	noDefaults          bool

	outbox          *outbox.Outbox
	outboxOpts      []outbox.Option
	outboxEnabled   bool
	outboxConsumers []outboxConsumerDecl

	fanout fanout.Fanout

	secret          []byte
	previousSecrets [][]byte

	secretOptionSet bool

	noAutoMigrate bool

	logger atomic.Pointer[slog.Logger]

	initialized atomic.Bool

	startHooks []func(ctx context.Context) error
	appCtx     context.Context
	appCancel  context.CancelFunc

	readyHooks []func(addr string)

	seedHooks []func(ctx context.Context) error

	lc *lifecycle.Lifecycle

	health *healthState

	flagEval     *featureflag.Evaluator
	flagMu       sync.Mutex
	flagAccessed bool

	idempotency *middleware.IdempotencyConfig

	translator *i18n.Translator

	localeResolver i18n.LocaleResolver

	localePrefixes bool
	pathLocales    i18n.PathLocales

	metrics *middleware.Metrics

	debugAuthorize func(ctx context.Context) bool

	tracing bool

	mcpIntrospection           bool
	mcpControl                 bool
	mcpControlGate             func(ctx context.Context) error
	localRunInfo               LocalRunInfo
	mcpRegistrars              []func(*mcp.Server) error
	mcpApps                    []mcp.AppConfig
	mcpMountDevImplied         bool
	mcpIntrospectionDevImplied bool
	devEntityMCPTools          sync.Map
	mcpControlDevImplied       bool
	mcpAutoMount               bool
	mcpGate                    func(ctx context.Context) error
	a2aCfg                     *A2AConfig
	a2a                        *a2a.Server
	a2aPath                    string
	oauthResource              *OAuthProtectedResourceConfig
	agentSkills                []AgentSkillEntry
	oauthAuthServer            *OAuthAuthorizationServerConfig
	authMD                     *AuthMDConfig
	authenticatorNoted         atomic.Bool
	webBotAuth                 *WebBotAuthConfig
	webBotAuthVerifier         *webbotauth.Verifier
	ucp                        *UCPConfig
	acp                        *ACPConfig

	startupOutput io.Writer

	role    Role
	roleOpt Role
	roleSet bool

	configDiscards []string

	setup SetupRunner

	handlerCell atomic.Pointer[servingHandler]

	serverWrappers []func(http.Handler) http.Handler // serverMu
	serverWrapped  bool

	txDialectMu  sync.Mutex
	txDialectVal migrate.Dialect
}

type servingHandler struct {
	h http.Handler
}

// AppOption is a functional option for configuring an App.
type AppOption func(*App)

// WithDB sets the database connection.
func WithDB(db *sql.DB) AppOption {
	return func(a *App) {
		a.DB = db
	}
}

// WithConfig replaces AppConfig wholesale, zero fields included: it overrides every earlier option and
// later granular setters override it. NewApp warns naming each earlier-set field it discards.
func WithConfig(config AppConfig) AppOption {
	return func(a *App) {
		a.configDiscards = append(a.configDiscards, discardedConfigFields(a.Config, config)...)
		a.Config = config
	}
}

func defaultAppConfig() AppConfig {
	return AppConfig{JSONCase: crud.CaseCamel}
}

func discardedConfigFields(old, replacement AppConfig) []string {
	def := reflect.ValueOf(defaultAppConfig())
	ov := reflect.ValueOf(old)
	nv := reflect.ValueOf(replacement)
	var out []string
	for i := 0; i < ov.NumField(); i++ {
		if !nv.Field(i).IsZero() {
			continue
		}
		if reflect.DeepEqual(ov.Field(i).Interface(), def.Field(i).Interface()) {
			continue
		}
		out = append(out, ov.Type().Field(i).Name)
	}
	return out
}

// WithAPIPrefix sets AppConfig.APIPrefix; "api", "/api" and "/api/" are the same prefix.
func WithAPIPrefix(prefix string) AppOption {
	return func(a *App) {
		a.Config.APIPrefix = prefix
	}
}

// WithPublicOpenAPI sets AppConfig.PublicOpenAPI.
func WithPublicOpenAPI() AppOption {
	return func(a *App) {
		a.Config.PublicOpenAPI = true
	}
}

// WithSecurityHeaders sets AppConfig.SecurityHeaders; unset fields keep the strict defaults.
func WithSecurityHeaders(cfg middleware.SecurityHeadersConfig) AppOption {
	return func(a *App) {
		a.Config.SecurityHeaders = cfg
	}
}

// WithHTTPServerTimeouts sets AppConfig.HTTPServerTimeouts.
func WithHTTPServerTimeouts(cfg HTTPServerTimeoutsConfig) AppOption {
	return func(a *App) {
		a.Config.HTTPServerTimeouts = cfg
	}
}

func (a *App) apiPrefix() string {
	p := strings.Trim(a.Config.APIPrefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// OpenAPISpec builds the /openapi.json document from the registry as it stands; title defaults to Config.Name.
func (a *App) OpenAPISpec(title string) *coreoa.Spec {
	if title == "" {
		title = a.Config.Name
	}
	if title == "" {
		title = "GoFastr API"
	}
	return openapi.EntityOpenAPIFor(a.Registry, openapi.Options{
		Title: title, Version: "1.0.0",
		CRUDMounted: a.entityCRUDEnabled, BulkMounted: a.entityUIMounted,
		ReadOnlyMounted: func(e *entity.Entity) bool { return a.entityCrudMount(e).ReadOnly },
		BasePath:        a.apiPrefix(), JSONCase: a.JSONCasing(),
	})
}

func (a *App) entityMountPath(table string) string {
	return a.apiPrefix() + "/" + table
}

type crudMount struct {
	r    *router.Router
	rel  string
	full string
}

func (a *App) recordCrudMount(e *entity.Entity, r *router.Router, rel, full string) {
	if a.crudMounts == nil {
		a.crudMounts = map[*entity.Entity]crudMount{}
	}
	a.crudMounts[e] = crudMount{r: r, rel: rel, full: full}
	a.mountEntityUIRoutes(e)
}

func (a *App) entityCRUDEnabled(e *entity.Entity) bool {
	return a.DB != nil && (e.Config.Exposure.CRUD == nil || *e.Config.Exposure.CRUD)
}

// EntityCRUDMounted reports whether e's CRUD routes were registered, read-only view mounts included.
func (a *App) EntityCRUDMounted(e *entity.Entity) bool {
	return a.entityCrudMount(e).Mounted
}

// EntityReadOnlyMounted reports whether e's mounted CRUD routes are read-only, as with a columned App.View.
func (a *App) EntityReadOnlyMounted(e *entity.Entity) bool {
	return a.entityCrudMount(e).ReadOnly
}

func (a *App) entityCrudMount(e *entity.Entity) crud.MountInfo {
	if !a.entityCRUDEnabled(e) {
		if a.apiPrefix() != "" && e.Version == "" {
			return crud.MountInfo{Path: a.entityMountPath(e.GetTable())}
		}
		return crud.MountInfo{}
	}
	path := a.crudMounts[e].full
	for _, v := range a.migrationViews {
		if len(v.Columns) > 0 && v.Name == e.GetTable() {
			return crud.MountInfo{Mounted: true, ReadOnly: true, Path: a.entityMountPath(e.GetTable())}
		}
	}
	return crud.MountInfo{Mounted: true, Path: path}
}

func (a *App) pageIndexPath() string {
	for _, rt := range a.router.Routes() {
		if rt.Method == http.MethodGet && path.Base(rt.Pattern) == "llm-pages.md" {
			return rt.Pattern
		}
	}
	return ""
}

// WithRouter sets a custom router.
func WithRouter(r *router.Router) AppOption {
	return func(a *App) {
		a.router = r
	}
}

// Router returns the router custom routes go on; patterns use net/http ServeMux syntax ({id}, not :id).
func (a *App) Router() *router.Router { return a.router }

// WithMCPServer sets a custom MCP server.
func WithMCPServer(s *mcp.Server) AppOption {
	return func(a *App) {
		a.MCP = s
	}
}

// WithMCPTools runs register on the MCP server during InitPlugins, before the introspection tools; an error fails the boot, nil panics.
func WithMCPTools(register func(*mcp.Server) error) AppOption {
	if register == nil {
		panic("framework.WithMCPTools: nil registrar")
	}
	return func(a *App) {
		a.mcpRegistrars = append(a.mcpRegistrars, register)
	}
}

// WithMCP serves the MCP server at /mcp over Streamable HTTP; mounting /mcp by hand as well panics.
func WithMCP() AppOption {
	return func(a *App) {
		a.mcpAutoMount = true
	}
}

// WithFileStorage sets the storage for Image and File fields; without it a multipart request on them fails.
func WithFileStorage(s upload.Storage) AppOption {
	return func(a *App) {
		a.Storage = s
	}
}

// WithStripUploadMetadata strips EXIF, XMP and text metadata from stored upload originals, applying orientation first.
func WithStripUploadMetadata() AppOption {
	return func(a *App) {
		a.stripUploadMetadata = true
	}
}

// WithImagePipeline derives renditions and a BlurHash for every schema.Image upload into the declared
// <field>_blurhash, _placeholder and _variants columns. Requires WithFileStorage.
func WithImagePipeline(d file.ImageDeriver) AppOption {
	return func(a *App) {
		a.imageDeriver = d
	}
}

// WithImagePipelineFor overrides WithImagePipeline for one entity field; a nil deriver opts the field out.
func WithImagePipelineFor(entityName, fieldName string, d file.ImageDeriver) AppOption {
	return func(a *App) {
		if a.fieldImageDerivers == nil {
			a.fieldImageDerivers = map[string]map[string]file.ImageDeriver{}
		}
		if a.fieldImageDerivers[entityName] == nil {
			a.fieldImageDerivers[entityName] = map[string]file.ImageDeriver{}
		}
		a.fieldImageDerivers[entityName][fieldName] = d
	}
}

// WithoutAutoMigrate skips the entity DDL Start runs. Seeds still run, so a seeded entity with no table fails Start.
func WithoutAutoMigrate() AppOption {
	return func(a *App) {
		a.noAutoMigrate = true
	}
}

// WithOutbox stages CRUD lifecycle events in the write transaction and relays them at least once to the
// WithOutboxConsumer consumers, not the live bus. Requires WithDB; opts go to outbox.New.
func WithOutbox(opts ...outbox.Option) AppOption {
	return func(a *App) {
		a.outboxEnabled = true
		a.outboxOpts = opts
	}
}

type outboxConsumerDecl struct {
	name      string
	eventType string
	handler   event.EventHandler
}

// WithOutboxConsumer declares a durable consumer, unique by (eventType, name), whose handler dedupes on Event.ID. Requires WithOutbox.
func WithOutboxConsumer(name, eventType string, handler event.EventHandler) AppOption {
	return func(a *App) {
		a.outboxConsumers = append(a.outboxConsumers, outboxConsumerDecl{name, eventType, handler})
	}
}

// WithFanout bridges the event bus and mounted UI hosts across replicas: every On/Subscribe handler then fires
// on every replica. The caller closes f after Shutdown. Panics on nil.
func WithFanout(f fanout.Fanout) AppOption {
	if f == nil {
		panic("framework: WithFanout(nil): construct a fanout first (framework/fanout.NewPostgres, core/fanout.NewRedis)")
	}
	return func(a *App) {
		a.fanout = f
	}
}

// WithGrantStore registers the RBAC GrantStore; with WithFanout a Grant or Revoke makes every replica
// re-read that role from the DB. Run EnsureSchema and LoadInto on it before Start.
func WithGrantStore(gs *access.GrantStore) AppOption {
	return func(a *App) {
		a.access = gs
	}
}

// WithoutDefaultMiddleware skips DefaultMiddleware, leaving the chain to Use.
func WithoutDefaultMiddleware() AppOption {
	return func(a *App) {
		a.noDefaults = true
	}
}

// WithLogger sets the App's logger, as SetLogger does. Panics if l is nil.
func WithLogger(l *slog.Logger) AppOption {
	if l == nil {
		panic("framework: WithLogger(nil): use slog.DiscardHandler to silence logging")
	}
	return func(a *App) {
		a.logger.Store(l)
	}
}

// WithIdempotency adds the Idempotency-Key middleware innermost; unset Principal,
// Tenant and Scope default to the caller, its tenant and its held scopes.
func WithIdempotency(cfg middleware.IdempotencyConfig) AppOption {
	if cfg.Principal == nil {
		cfg.Principal = ownerPrincipal
	}
	if cfg.Tenant == nil {
		cfg.Tenant = requestTenantID
	}
	if cfg.Scope == nil {
		cfg.Scope = heldScopeKey
	}
	return func(a *App) {
		config := cfg
		a.idempotency = &config
	}
}

func requestTenantID(r *http.Request) string {
	return handler.GetTenantID(r.Context())
}

func heldScopeKey(r *http.Request) string {
	held, scoped := access.HeldScopes(r.Context())
	if !scoped {
		return ""
	}
	names := make([]string, len(held))
	for i, p := range held {
		names[i] = string(p)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var b strings.Builder
	b.WriteString("scoped:")
	for _, n := range names {
		b.WriteString(strconv.Itoa(len(n)))
		b.WriteByte(':')
		b.WriteString(n)
	}
	return b.String()
}

func ownerPrincipal(r *http.Request) string {
	if id, ok := owner.Get(r.Context()); ok && id != nil {
		return fmt.Sprintf("%v", id)
	}
	if k := credentialFingerprint(r); k != "" {
		return k
	}
	return "anon"
}

func credentialFingerprint(r *http.Request) string {
	h := sha256.New()
	wrote := false
	for _, name := range []string{"Authorization", "X-API-Key", fembed.GrantHeader, "Cookie"} {
		for _, v := range r.Header.Values(name) {
			if v == "" {
				continue
			}
			_, _ = h.Write([]byte(name))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte(v))
			_, _ = h.Write([]byte{0})
			wrote = true
		}
	}
	if !wrote {
		return ""
	}
	return "cred:" + hex.EncodeToString(h.Sum(nil)[:8])
}

// WithMetrics adds request metrics to the default chain and mounts an
// unauthenticated /metrics endpoint. Incompatible with WithoutDefaultMiddleware.
func WithMetrics() AppOption {
	return func(a *App) {
		a.metrics = middleware.NewMetrics()
	}
}

// WithDebugAuthorize decides which authenticated caller may read the debug
// endpoints, false answering 403; the default requires the "admin" role.
func WithDebugAuthorize(fn func(ctx context.Context) bool) AppOption {
	return func(a *App) {
		a.debugAuthorize = fn
	}
}

// WithTracing adds OpenTelemetry tracing to the default chain. Incompatible
// with WithoutDefaultMiddleware.
func WithTracing() AppOption {
	return func(a *App) {
		a.tracing = true
	}
}

// WithI18n installs tr, also as i18n.Default, and adds locale negotiation to
// the default chain. Incompatible with WithoutDefaultMiddleware.
func WithI18n(tr *i18n.Translator) AppOption {
	return func(a *App) {
		a.translator = tr
		i18n.SetDefault(tr)
	}
}

// WithLocaleResolver installs a resolver consulted before the locale headers,
// such as [i18n.CookieLocale]. Panics unless an earlier option is WithI18n.
func WithLocaleResolver(r i18n.LocaleResolver) AppOption {
	return func(a *App) {
		if a.translator == nil {
			panic("framework: WithLocaleResolver requires WithI18n: install a translator first")
		}
		a.localeResolver = r
	}
}

// Logger returns the App's logger, never nil and independent of slog.Default.
func (a *App) Logger() *slog.Logger {
	return a.logger.Load()
}

// SetLogger replaces the App's logger while requests run. Panics if l is nil.
func (a *App) SetLogger(l *slog.Logger) {
	if l == nil {
		panic("framework: App.SetLogger(nil): use slog.DiscardHandler to silence logging")
	}
	a.logger.Store(l)
}

// DefaultMiddleware returns the chain NewApp installs, recovery outermost and
// the request timeout innermost. a may be nil in tests.
func DefaultMiddleware(a *App) []router.Middleware {
	var getLogger func() *slog.Logger
	timeout := defaultRequestTimeout
	var translator *i18n.Translator
	var localeResolver i18n.LocaleResolver
	if a != nil {
		getLogger = a.Logger
		if a.Config.RequestTimeout > 0 {
			timeout = a.Config.RequestTimeout
		}
		translator = a.translator
		localeResolver = a.localeResolver
	}
	chain := []router.Middleware{
		middleware.RecoveryFn(getLogger),
		middleware.RequestID(),
	}
	if translator != nil {
		var negOpts []i18n.NegotiateOption
		if localeResolver.Resolve != nil {
			negOpts = append(negOpts, i18n.WithLocaleResolver(localeResolver))
		}
		chain = append(chain, i18n.Middleware(translator, negOpts...), middleware.RecoveryFn(getLogger))
	}
	if a != nil && a.Config.Compression {
		chain = append(chain, middleware.Compress())
	}
	if a != nil && a.localePrefixes {
		chain = append(chain, urlprefix.Middleware())
	}
	if a != nil && a.DB != nil {
		chain = append(chain, a.DBContextMiddleware())
	}
	if a != nil {
		chain = append(chain, a.FlagContextMiddleware())
	}
	if a != nil && a.tracing {
		chain = append(chain, middleware.Tracing())
	}
	if a != nil && a.metrics != nil {
		chain = append(chain, middleware.MetricsMiddleware(a.metrics))
	}
	shCfg := middleware.SecurityHeadersConfig{}
	if a != nil {
		shCfg = a.Config.SecurityHeaders
	}
	chain = append(chain, middleware.SecurityHeaders(shCfg))
	if a == nil || !a.Config.DisableRequestTimeout {
		chain = append(chain, middleware.Timeout(timeout))
	}
	return chain
}

// Mount registers m's routes now and hands it the app's fanout and keys.
// Panics on a route that is an entity's CRUD path or keys it cannot derive.
func (a *App) Mount(m Mountable) *App {
	if provider, ok := m.(interface{ RoutePatterns() []string }); ok {
		for _, ent := range a.Registry.AllSorted() {
			if !a.entityCRUDEnabled(ent) {
				continue
			}
			mountPath := a.entityMountPath(ent.GetTable())
			for _, pattern := range provider.RoutePatterns() {
				if strings.TrimRight(pattern, "/") == strings.TrimRight(mountPath, "/") {
					panic("framework: " + entityScreenCollisionMessage(ent.Config.Name, mountPath, pattern))
				}
			}
		}
	}
	a.mountables = append(a.mountables, m)
	if a.localePrefixes {
		if pl, ok := m.(interface{ SetPathLocales(i18n.PathLocales) }); ok {
			pl.SetPathLocales(a.pathLocales)
		}
	}
	m.Mount(a.router)
	if a.fanout != nil {
		if fm, ok := m.(interface {
			SetFanout(fanout.Fanout) (func(), error)
		}); ok {
			stop, err := fm.SetFanout(a.fanout)
			if err != nil {
				panic("framework: WithFanout: wiring mounted host: " + err.Error())
			}
			a.OnStop(func() error {
				stop()
				return nil
			})
		}
	}
	switch s := m.(type) {
	case interface {
		SetSessionKeys(current []byte, previous ...[]byte)
	}:
		currentKey, previousKeys, err := sessionKeysForMount(a.secret, a.previousSecrets, a.fanout != nil)
		if err != nil {
			panic(err.Error())
		}
		if currentKey != nil {
			s.SetSessionKeys(currentKey, previousKeys...)
		}
	case interface{ SetSessionKey([]byte) }:
		currentKey, _, err := sessionKeysForMount(a.secret, a.previousSecrets, a.fanout != nil)
		if err != nil {
			panic(err.Error())
		}
		if currentKey != nil {
			s.SetSessionKey(currentKey)
		}
	}
	if eh, ok := m.(interface{ EmbedHost() *fembed.Host }); ok {
		if host := eh.EmbedHost(); host != nil {
			nonceKey, grantKey, err := embedKeysForMount(a.secret)
			if err != nil {
				panic(err.Error())
			}
			host.SetKeys(nonceKey, grantKey)
			if prevNonce, prevGrant := embedPreviousKeysForMount(a.previousSecrets); len(prevNonce) > 0 {
				host.SetPreviousKeys(prevNonce, prevGrant)
			}
			a.embedHost = host
		}
	}
	return a
}

// EmbedReserving is implemented by a Mountable, plugin or battery whose routes
// an embed grant must never reach; it reports the prefixes it actually mounted.
type EmbedReserving interface {
	ReservedEmbedPrefixes() []string
}

// BootValidator is a Mountable whose ValidateBoot Start calls once every route
// is registered, before the listener binds; a panic aborts the boot.
type BootValidator interface {
	ValidateBoot()
}

func (a *App) reserveEmbedPrefixes() {
	if a.embedHost == nil {
		return
	}
	var prefixes []string
	for _, m := range a.mountables {
		if er, ok := m.(EmbedReserving); ok {
			prefixes = append(prefixes, er.ReservedEmbedPrefixes()...)
		}
	}
	for _, p := range a.Plugins.All() {
		if er, ok := p.(EmbedReserving); ok {
			prefixes = append(prefixes, er.ReservedEmbedPrefixes()...)
		}
	}
	for _, b := range a.Batteries.All() {
		if er, ok := b.(EmbedReserving); ok {
			prefixes = append(prefixes, er.ReservedEmbedPrefixes()...)
		}
	}
	if len(prefixes) == 0 {
		return
	}
	if err := a.embedHost.AddReservedPrefixes(prefixes...); err != nil {
		panic(err.Error())
	}
}

// Mountables returns a copy of the Mountables registered via Mount, in order.
func (a *App) Mountables() []Mountable {
	out := make([]Mountable, len(a.mountables))
	copy(out, a.mountables)
	return out
}

// Group creates a route group under prefix.
func (a *App) Group(prefix string, opts ...routegroup.GroupOption) *routegroup.RouteGroup {
	return routegroup.New(a.router, prefix, opts...)
}

// GroupEntity is App.Entity under g's prefix, MCP namespace and OpenAPI tag.
// Panics on any misconfiguration.
func (a *App) GroupEntity(g *routegroup.RouteGroup, name string, config entity.EntityConfig) *App {
	if queued := a.pendingEndpoints[name]; len(queued) > 0 {
		config.Endpoints = append(slices.Clip(config.Endpoints), queued...)
	}
	e := entity.Define(name, config)
	e.Version = g.Prefix()
	e.OpenAPITag = g.OpenAPITag()

	crudEnabled := a.entityCRUDEnabled(e)
	if a.DB != nil {
		if err := ValidateExposure(name, e.Config); err != nil {
			panic("framework: " + err.Error())
		}
	}
	mcpTools := (e.Config.Exposure.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil

	crudMount := ""
	if crudEnabled {
		crudMount = g.Prefix() + "/" + e.GetTable()
		if msg := a.entityRouteCollision(e, crudMount); msg != "" {
			panic("framework: " + msg)
		}
	}
	if err := a.validateEntityRegistration(e, config.Endpoints, mcpTools, crudMount, groupMount(g, e)); err != nil {
		panic(fmt.Sprintf("framework: failed to register entity %q in group %q: %v", name, g.Prefix(), err))
	}
	if a.DB != nil {
		e.SetDB(a.DB)
	}

	if err := a.Registry.Register(e); err != nil {
		panic(fmt.Sprintf("framework: failed to register entity %q in group %q: %v", name, g.Prefix(), err))
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		crudHandler = a.wireCrudHandler(e)
		crudHandler.BasePath = g.Prefix()
		crudHandler.MCPNamespace = g.MCPNamespace()

		crud.RegisterCrudRoutes(g.Router(), crudHandler, "/"+e.GetTable(), crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
		a.recordCrudMount(e, g.Router(), "/"+e.GetTable(), crudMount)
	}

	if mcpTools {
		crudHandler.MCPRouteScoped = true
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, g.Router()); err != nil {
			panic(fmt.Sprintf("framework: failed to register MCP tools for entity %q in group %q: %v", name, g.Prefix(), err))
		}
		a.rememberDevEntityMCPTools(e, groupMount(g, e))
	}

	if len(config.Endpoints) > 0 {
		if err := a.registerGroupEndpoints(g, e, config.Endpoints); err != nil {
			panic(fmt.Sprintf("framework: failed to register endpoints for entity %q in group %q: %v", name, g.Prefix(), err))
		}
	}

	a.claimQueuedEndpoints(name)
	return a
}

func (a *App) registerGroupEndpoints(g *routegroup.RouteGroup, ent *entity.Entity, endpoints []entity.Endpoint) error {
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		path := groupEndpointPath(ent, endpoint)
		if endpoint.Handler != nil {
			crud.EntityRouter(g.Router(), ent).Handle(method, path, endpointRoute(endpoint))
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
			if err := a.MCP.RegisterTool(toolName, description, openapi.EndpointInputSchema(endpoint), endpointMCPTwin(g.Prefix()+path, endpoint.MCPHandler), endpointMCPOptions(ent, endpoint)...); err != nil {
				return err
			}
		}
	}
	return nil
}

func groupEndpointPath(ent *entity.Entity, endpoint entity.Endpoint) string {
	path := "/" + strings.Trim(ent.GetTable(), "/")
	if !strings.HasPrefix(strings.TrimSpace(endpoint.Path), "/") {
		path += "/" + strings.TrimPrefix(endpoint.Path, "/")
	} else {
		path = strings.TrimSpace(endpoint.Path)
	}
	return crud.NormalizePath(convertGroupEndpointPath(path))
}

type registrationMount struct {
	endpointPaths func(entity.Endpoint) (route, toolPath string)
	mcpNamespace  string
}

func (a *App) plainMount(ent *entity.Entity) registrationMount {
	return registrationMount{endpointPaths: func(ep entity.Endpoint) (string, string) {
		return openapi.EntityEndpointRoutePath(ent, ep.Path, a.apiPrefix()), openapi.EntityEndpointPath(ent, ep.Path)
	}}
}

func groupMount(g *routegroup.RouteGroup, ent *entity.Entity) registrationMount {
	return registrationMount{
		endpointPaths: func(ep entity.Endpoint) (string, string) {
			p := g.Prefix() + groupEndpointPath(ent, ep)
			return p, p
		},
		mcpNamespace: g.MCPNamespace(),
	}
}

func (m registrationMount) entityTool(ent, action string) string {
	if m.mcpNamespace == "" {
		return ent + "_" + action
	}
	return m.mcpNamespace + "." + ent + "." + action
}

func (m registrationMount) endpointTool(name string) string {
	if m.mcpNamespace == "" {
		return name
	}
	return m.mcpNamespace + "." + name
}

func convertGroupEndpointPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

// WrapServer wraps everything Start serves, not only the router; the first
// wrapper is outermost. Panics once Start has built the server.
func (a *App) WrapServer(wrap func(http.Handler) http.Handler) *App {
	if wrap == nil {
		return a
	}
	a.serverMu.Lock()
	defer a.serverMu.Unlock()
	if a.server != nil || a.serverWrapped {
		panic("framework: WrapServer called after Start bound the server")
	}
	a.serverWrappers = append(a.serverWrappers, wrap)
	return a
}

// Use appends middleware to the router chain, wrapping earlier routes too.
func (a *App) Use(mw ...router.Middleware) *App {
	if len(mw) == 0 {
		return a
	}
	a.router.Use(mw...)
	return a
}

// DefaultDotEnvPaths returns the dotenv files NewApp loads, earliest winning:
// .env.local, .env.<APP_ENV> when APP_ENV is set, then .env.
func DefaultDotEnvPaths() []string { return defaultDotEnvPaths() }

func defaultDotEnvPaths() []string {
	paths := []string{".env.local"}
	if appEnv := os.Getenv("APP_ENV"); appEnv != "" {
		paths = append(paths, ".env."+appEnv)
	}
	return append(paths, ".env")
}

// NewApp creates an App, first loading DefaultDotEnvPaths unless
// GOFASTR_DOTENV=off. Panics on an invalid combination of options.
func NewApp(opts ...AppOption) *App {
	if os.Getenv("GOFASTR_DOTENV") != "off" {
		_ = dotenv.LoadAndApply(defaultDotEnvPaths()...)
	}

	a := &App{
		health:        &healthState{},
		Registry:      NewRegistry(),
		router:        router.New(),
		MCP:           mcp.NewServer(),
		Config:        defaultAppConfig(),
		Plugins:       NewPluginManager(),
		Batteries:     NewBatteryManager(),
		events:        event.NewEventBus(),
		hooks:         make(map[string]*hook.HookRegistry),
		lc:            lifecycle.New(),
		startupOutput: os.Stdout,
	}
	a.Registry.handlerFor = a.CrudHandlerForEntity
	a.Batteries.logger = a.Logger

	for _, opt := range opts {
		opt(a)
	}
	a.trustedProxies = clientip.ParseProxies(a.Config.TrustedProxies)
	if a.mcpGate != nil && a.MCP != nil {
		a.MCP.SetGate(a.mcpGate)
	}
	if a.secret == nil {
		if env := os.Getenv("GOFASTR_SECRET"); env != "" {
			a.secret = []byte(validateSecret(env))
		}
	}
	if !a.secretOptionSet {
		if env := os.Getenv("GOFASTR_SECRET_PREVIOUS"); env != "" {
			for _, p := range splitSecretList(env) {
				a.previousSecrets = append(a.previousSecrets, []byte(validateSecret(p)))
			}
		}
	}
	role, err := resolveRole(a.roleOpt, a.roleSet)
	if err != nil {
		panic(err.Error())
	}
	a.role = role

	if a.logger.Load() == nil {
		a.logger.Store(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	if len(a.configDiscards) > 0 {
		final := reflect.ValueOf(a.Config)
		seen := make(map[string]bool, len(a.configDiscards))
		var lost []string
		for _, name := range a.configDiscards {
			if seen[name] {
				continue
			}
			seen[name] = true
			if f := final.FieldByName(name); f.IsValid() && f.IsZero() {
				lost = append(lost, name)
			}
		}
		if len(lost) > 0 {
			a.Logger().Warn("WithConfig replaced AppConfig fields set by earlier options: WithConfig replaces the whole config, so options placed before it are discarded. Move the granular options after WithConfig, or set these fields in the AppConfig literal.",
				"discarded", strings.Join(lost, ", "))
		}
		a.configDiscards = nil
	}

	if len(a.outboxConsumers) > 0 && !a.outboxEnabled {
		panic("framework: WithOutboxConsumer requires WithOutbox: enable the outbox for durable delivery")
	}
	if a.outboxEnabled {
		if a.DB == nil {
			panic("framework: WithOutbox requires WithDB: the outbox stages event rows in the entity write transaction")
		}
		ob, err := outbox.New(a.DB, a.outboxOpts...)
		if err != nil {
			panic("framework: WithOutbox: " + err.Error())
		}
		for _, c := range a.outboxConsumers {
			ob.Consume(c.name, c.eventType, c.handler)
		}
		a.outbox = ob
		a.outboxConsumers = nil
	}

	if a.fanout != nil {
		stopBridge, err := event.AttachFanout(a.events, a.fanout)
		if err != nil {
			panic("framework: WithFanout: " + err.Error())
		}
		a.OnStop(func() error {
			stopBridge()
			return nil
		})
	}

	a.modules = NewModuleManager(a.DB, a.fanout)
	a.router.SetRegisterHook(func(method, pattern string) {
		a.modules.recordRoute(method, pattern)
	})
	a.router.SetRouteGate(func(key string) bool {
		return a.modules.routeAllowed(key)
	})
	a.MCP.SetRegisterHook(func(toolName string) {
		a.devEntityMCPTools.Delete(toolName)
		a.modules.recordTool(toolName)
	})
	a.MCP.SetCallGate(func(toolName string) error {
		return a.modules.toolAllowed(toolName)
	})
	if a.fanout != nil {
		if err := a.modules.subscribeFanout(); err != nil {
			panic("framework: module fanout subscribe: " + err.Error())
		}
	}
	if a.fanout != nil && a.access != nil {
		stop, err := a.access.SetFanout(a.fanout)
		if err != nil {
			panic("framework: access fanout subscribe: " + err.Error())
		}
		a.OnStop(func() error {
			stop()
			return nil
		})
	}

	if a.idempotency != nil && a.idempotency.Store == nil && a.fanout != nil {
		a.Logger().Warn("framework: WithIdempotency keeps its keys in this process's memory, but WithFanout runs the app on several replicas: " +
			"a retry that reaches another replica runs the handler again; set IdempotencyConfig.Store to a shared store (middleware.NewSQLIdempotencyStore)")
	}
	if a.idempotency != nil && a.idempotency.Store == nil {
		opts := []middleware.MemoryIdempotencyOption{
			middleware.WithMemoryStoreMaxEntries(a.idempotency.MaxStoreEntries),
			middleware.WithMemoryStoreMaxEntryBytes(a.idempotency.MaxStoreEntryBytes),
		}
		if budget := a.requestBudget(); budget > 0 {
			opts = append(opts, middleware.WithMemoryIdempotencyInFlightTTL(budget+idempotencyInFlightSlack))
		}
		a.idempotency.Store = middleware.NewMemoryIdempotencyStore(a.idempotency.TTL, opts...)
	}
	if a.fanout != nil && a.DB == nil {
		a.Logger().Warn("framework: module toggles remain local with WithFanout but no WithDB: " +
			"refresh messages re-read each replica's in-memory store; use WithDB against a shared database to propagate toggles")
	}
	if a.noDefaults && a.translator != nil {
		panic("framework: WithI18n is incompatible with WithoutDefaultMiddleware: " +
			"the i18n middleware lives in the default chain; mount it explicitly via " +
			"router.Middleware(i18n.Middleware(translator)) in your custom chain instead")
	}
	if a.localePrefixes {
		a.installLocalePrefixes()
	}
	if a.noDefaults && a.metrics != nil {
		panic("framework: WithMetrics is incompatible with WithoutDefaultMiddleware: " +
			"mount middleware.MetricsMiddleware + middleware.MetricsHandler in your custom chain instead")
	}
	if a.noDefaults && a.tracing {
		panic("framework: WithTracing is incompatible with WithoutDefaultMiddleware: " +
			"mount middleware.Tracing() in your custom chain instead")
	}

	if !a.noDefaults {
		a.router.Use(DefaultMiddleware(a)...)
	}
	if proxies := a.trustedProxies; !proxies.Empty() {
		a.router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(clientip.WithTrusted(r.Context(), proxies)))
			})
		})
	}
	if a.idempotency != nil {
		a.router.UseInner(middleware.Idempotency(*a.idempotency))
	}

	if a.webBotAuth != nil && a.webBotAuth.Verify != nil {
		a.webBotAuthVerifier = webbotauth.New(a.webBotAuth.Verify.Require, a.Logger(), webbotauth.WithMaxValidity(a.webBotAuth.Verify.MaxValidity))
		a.router.Use(a.webBotAuthVerifier.Middleware)
	}

	a.router.Use(retired.Middleware(dev.Enabled))

	if a.DB != nil {
		a.Registry.SetDB(a.DB)
	}

	dev.MaybeRegisterLiveReload(a.router)
	dev.MaybeRegisterLayoutLayer(a.router)

	if dev.DevMCPEnabled() {
		if !a.mcpAutoMount {
			a.mcpAutoMount, a.mcpMountDevImplied = true, true
		}
		if a.MCP != nil {
			a.MCP.SetRequireLoopbackHost(true)
		}
		if !a.mcpIntrospection {
			a.mcpIntrospection, a.mcpIntrospectionDevImplied = true, true
		}
		if !a.mcpControl {
			a.mcpControl, a.mcpControlDevImplied = true, true
		}
	}

	return a
}

// NewUIHostApp is NewApp(opts...).Mount(host).
func NewUIHostApp(host Mountable, opts ...AppOption) *App {
	return NewApp(opts...).Mount(host)
}

// Entity is TryEntity that panics on the error.
func (a *App) Entity(name string, config entity.EntityConfig) *App {
	if err := a.TryEntity(name, config); err != nil {
		panic("framework: " + err.Error())
	}
	return a
}

func (a *App) checkQueuedEndpointsClaimed() error {
	var unclaimed []string
	for _, name := range slices.Sorted(maps.Keys(a.pendingEndpoints)) {
		if !a.claimedEndpoints[name] {
			unclaimed = append(unclaimed, name)
		}
	}
	if len(unclaimed) > 0 {
		return fmt.Errorf("EntityEndpoints queued endpoints for %s, which no App.Entity, TryEntity, or GroupEntity call registered", strings.Join(unclaimed, ", "))
	}
	return nil
}

func (a *App) claimQueuedEndpoints(name string) {
	if len(a.pendingEndpoints[name]) == 0 {
		return
	}
	if a.claimedEndpoints == nil {
		a.claimedEndpoints = map[string]bool{}
	}
	a.claimedEndpoints[name] = true
}

// EntityEndpoints queues endpoints for later registrations of name. Panics once
// name is registered; Start fails while a queued name is never registered.
func (a *App) EntityEndpoints(name string, endpoints ...entity.Endpoint) *App {
	if _, err := a.Registry.Get(name); err == nil || a.claimedEndpoints[name] {
		panic(fmt.Sprintf("framework: EntityEndpoints(%q) after the entity was registered; queue endpoints before App.Entity, TryEntity, or GroupEntity", name))
	}
	if a.pendingEndpoints == nil {
		a.pendingEndpoints = map[string][]entity.Endpoint{}
	}
	a.pendingEndpoints[name] = append(a.pendingEndpoints[name], endpoints...)
	return a
}

// TryEntity registers an entity, returning any misconfiguration as an error;
// a rejected declaration leaves nothing registered.
func (a *App) TryEntity(name string, config entity.EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("entity %q: %v", name, textsafe.Recovered(r))
		}
	}()

	if config.SeedFS != nil && config.SeedPath == "" {
		return fmt.Errorf("entity %q has SeedFS set but SeedPath empty: point SeedPath at a file within the FS or unset SeedFS", name)
	}

	queued := a.pendingEndpoints[name]
	if len(queued) > 0 {
		config.Endpoints = append(slices.Clip(config.Endpoints), queued...)
	}

	e := entity.Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	crudEnabled := a.entityCRUDEnabled(e)
	if a.DB != nil {
		if err := ValidateExposure(name, e.Config); err != nil {
			return err
		}
	}

	var mountPath string
	if crudEnabled {
		mountPath = a.entityMountPath(e.GetTable())
		if msg := a.entityRouteCollision(e, mountPath); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}

	mcpToolsEnabled := (e.Config.Exposure.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil
	if verr := a.validateEntityRegistration(e, config.Endpoints, mcpToolsEnabled, mountPath, a.plainMount(e)); verr != nil {
		return fmt.Errorf("entity %q: %w", name, verr)
	}

	if a.modules != nil {
		a.modules.recordEntity(name)
	}

	if err := a.Registry.Register(e); err != nil {
		return fmt.Errorf("failed to register entity %q: %w", name, err)
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		crudHandler = a.wireCrudHandler(e)
		crudHandler.BasePath = a.apiPrefix()
		crud.RegisterCrudRoutes(a.router, crudHandler, mountPath, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
		a.recordCrudMount(e, a.router, mountPath, mountPath)
	}

	if mcpToolsEnabled {
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, a.router); err != nil {
			return fmt.Errorf("failed to register MCP tools for entity %q: %w", name, err)
		}
		a.rememberDevEntityMCPTools(e, a.plainMount(e))
	}

	if len(config.Endpoints) > 0 {
		if err := a.registerEntityEndpoints(e, config.Endpoints); err != nil {
			return fmt.Errorf("failed to register endpoints for entity %q: %w", name, err)
		}
	}
	a.claimQueuedEndpoints(name)

	return nil
}

// CrudHandler returns the in-process CRUD handler for entity name. Errors when
// name is not registered or the app has no DB.
func (a *App) CrudHandler(name string) (*crud.CrudHandler, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("app.CrudHandler: no DB configured")
	}
	ent, err := a.Registry.Get(name)
	if err != nil {
		return nil, fmt.Errorf("app.CrudHandler: %w", err)
	}
	return a.wireCrudHandler(ent), nil
}

// wireCrudHandler is the one constructor behind every entity's CRUD handler:
// the mounted routes (App.Entity, App.GroupEntity), CrudHandler and
// CrudHandlerForEntity. Each used to copy the field list, and
// CrudHandlerForEntity's copy dropped ChildHooks, the image pipeline and the
// metadata strip, so the entityui back office saved images with no renditions
// and cascaded without child hooks. Callers set BasePath for their own mount.
func (a *App) wireCrudHandler(ent *entity.Entity) *crud.CrudHandler {
	name := ent.GetName()
	ch := crud.NewCrudHandler(ent, a.DB).WithJSONCase(a.JSONCasing())
	ch.Hooks = a.HookRegistry(name)
	ch.ChildHooks = a.lookupHookRegistry
	ch.Storage = a.Storage
	ch.ImageDeriver = a.imageDeriver
	ch.FieldImageDerivers = a.fieldImageDerivers[name]
	ch.StripUploadMetadata = a.stripUploadMetadata
	ch.Events = a.Events()
	if a.outbox != nil {
		ch.Outbox = a.outbox
	}
	ch.Registry = a.Registry
	return ch
}

// CrudHandlerForEntity is CrudHandler for an entity you already hold, such
// as one of several registered versions. It errors when ent is not the
// instance the registry holds at its name and version.
func (a *App) CrudHandlerForEntity(ent *entity.Entity) (*crud.CrudHandler, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: no DB configured")
	}
	if ent == nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: entity is nil")
	}
	registered, err := a.Registry.GetVersioned(ent.Config.Name, ent.Version)
	if err != nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: %w", err)
	}
	if registered != ent {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: entity %q at version %q is not the registered instance", ent.Config.Name, ent.Version)
	}
	ch := a.wireCrudHandler(ent)
	if ent.Version != "" {
		ch.BasePath = ent.Version
	} else {
		ch.BasePath = a.apiPrefix()
	}
	return ch, nil
}

// MustCrudHandler is CrudHandler that panics on error.
func (a *App) MustCrudHandler(name string) *crud.CrudHandler {
	ch, err := a.CrudHandler(name)
	if err != nil {
		panic(err)
	}
	return ch
}

// Table registers a raw table for migration only: no CRUD, routes,
// validation or injected columns. It panics when registration fails.
func (a *App) Table(t migrate.Table) *App {
	e := t.ToEntity()
	if a.DB != nil {
		e.SetDB(a.DB)
	}
	if err := a.Registry.Register(e); err != nil {
		panic(fmt.Sprintf("framework: failed to register table %q: %v", t.Name, err))
	}
	return a
}

// Routine registers a stored routine, whose Up runs on every boot after
// tables are migrated and which `migrate generate` tracks.
func (a *App) Routine(r migrate.Routine) *App {
	a.migrationRoutines = append(a.migrationRoutines, r)
	return a
}

// RoutinesFS registers every routine migrate.RoutinesFS loads from dir in
// fsys, and panics on a loader error.
func (a *App) RoutinesFS(fsys fs.FS, dir string) *App {
	rs, err := migrate.RoutinesFS(fsys, dir)
	if err != nil {
		panic(fmt.Sprintf("framework: App.RoutinesFS(%q): %v", dir, err))
	}
	a.migrationRoutines = append(a.migrationRoutines, rs...)
	return a
}

// View registers a database view, created on boot after its source
// tables. A view that declares Columns is also a read-only entity with
// read routes only. It panics on a misdeclared view.
func (a *App) View(v migrate.View) *App {
	a.migrationViews = append(a.migrationViews, v)

	ent := v.ToEntity()
	if ent == nil {
		return a
	}
	if err := ent.CheckRecordKey(); err != nil {
		panic(fmt.Sprintf("framework: view %q: %v", v.Name, err))
	}
	if a.DB != nil {
		ent.SetDB(a.DB)
	}
	if err := a.Registry.Register(ent); err != nil {
		panic(fmt.Sprintf("framework: failed to register view %q: %v", v.Name, err))
	}
	if a.DB != nil {
		ch := crud.NewCrudHandler(ent, a.DB).WithJSONCase(a.JSONCasing())
		ch.Registry = a.Registry
		ch.ChildHooks = a.lookupHookRegistry
		ch.Events = a.Events()
		ch.BasePath = a.apiPrefix()
		crud.RegisterCrudRoutes(a.router, ch, a.entityMountPath(ent.GetTable()),
			crud.CrudRouteOptions{ReadOnly: true, NoLLMMD: a.Config.NoLLMMD})
	}
	return a
}

// MigrationPlan returns the Plan Start migrates on boot: the registry and
// every registered routine and view. Pass it to migrate.GenerateMigrationFile
// to generate migrations from a host binary.
func (a *App) MigrationPlan() migrate.Plan {
	return migrate.Plan{Registry: a.Registry, Views: a.migrationViews, Routines: a.migrationRoutines}
}

// RegisterEntities calls App.Entity for each pair, in name order.
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

// entityRouteCollision returns a diagnostic when the entity's CRUD routes
// at mountPath would collide with a registered route, or "" when none do.
func (a *App) entityRouteCollision(ent *entity.Entity, mountPath string) string {
	name := ent.Config.Name
	mountPath = strings.TrimRight(mountPath, "/")
	if mountPath == "" {
		return ""
	}
	claimed := map[string][]string{}
	patterns := crud.CrudRoutePatterns(mountPath, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD, States: ent.Config.States, Key: ent.KeyColumns()})
	for _, pattern := range patterns {
		method, path, _ := strings.Cut(pattern, " ")
		path = normalizeRoutePattern(path)
		claimed[path] = append(claimed[path], method)
	}
	routes := a.router.Routes()
	for _, rt := range routes {
		methods, ok := claimed[normalizeRoutePattern(rt.Pattern)]
		if !ok {
			continue
		}
		rtMethod := strings.ToUpper(strings.TrimSpace(rt.Method))
		for _, m := range methods {
			if rtMethod == "" || m == rtMethod {
				return entityScreenCollisionMessage(name, mountPath, rt.Pattern)
			}
		}
	}
	held := newMuxReplay(routes)
	for _, pattern := range patterns {
		if conflict, refused := held.add(pattern, "this entity's own generated CRUD route"); refused {
			return entityScreenCollisionMessage(name, mountPath, conflict.pattern)
		}
	}
	return ""
}

// muxReplay is a scratch ServeMux holding the router's routes and the
// patterns added since, each with its owner.
type muxReplay struct {
	mux  *http.ServeMux
	held []heldPattern
}

type heldPattern struct{ pattern, owner string }

func newMuxReplay(routes []router.RegisteredRoute) *muxReplay {
	r := &muxReplay{mux: http.NewServeMux()}
	for _, rt := range routes {
		p := rt.Method + " " + rt.Pattern
		muxAccepts(r.mux, p)
		r.held = append(r.held, heldPattern{strings.TrimSpace(p), "an existing route"})
	}
	return r
}

func (r *muxReplay) add(pattern, owner string) (heldPattern, bool) {
	if muxAccepts(r.mux, pattern) {
		r.held = append(r.held, heldPattern{pattern, owner})
		return heldPattern{}, false
	}
	for _, h := range r.held {
		pair := http.NewServeMux()
		muxAccepts(pair, h.pattern)
		if !muxAccepts(pair, pattern) {
			return h, true
		}
	}
	return heldPattern{pattern: "(the routes registered together)", owner: "a combination of routes"}, true
}

func muxAccepts(mux *http.ServeMux, pattern string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	mux.Handle(pattern, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	return true
}

func normalizeRoutePattern(pattern string) string {
	segments := strings.Split(pattern, "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if strings.HasSuffix(seg, "...}") {
				segments[i] = "{...}"
			} else {
				segments[i] = "{}"
			}
		}
	}
	return strings.Join(segments, "/")
}

func entityScreenCollisionMessage(name, mountPath, screenPath string) string {
	return fmt.Sprintf(
		"entity %q would mount CRUD routes at %s (REST + %s/llm.md), "+
			"but a screen/route is already registered at %q. "+
			"Choose a different page path (e.g. /library, /library/{slug}), "+
			"rename the entity table, or move entity CRUD under an APIPrefix "+
			"(framework.WithAPIPrefix(\"/api\")) so the URL spaces don't collide.",
		name, mountPath, mountPath, screenPath)
}

func (a *App) validateEntityRegistration(ent *entity.Entity, endpoints []entity.Endpoint, mcpTools bool, crudMount string, m registrationMount) error {
	if err := validateDisplayQueries(ent); err != nil {
		return err
	}
	if a.entityUI != nil {
		if err := a.entityUI.CheckEntity(ent); err != nil {
			return err
		}
	}
	if len(endpoints) > 0 || (a.entityUI != nil && crudMount != "" && a.entityUIWouldOwn(ent)) {
		resolved := make([]Endpoint, 0, len(endpoints))
		for _, ep := range endpoints {
			if strings.TrimSpace(ep.Method) == "" || ep.Handler == nil {
				continue
			}
			ep.Path, _ = m.endpointPaths(ep)
			resolved = append(resolved, ep)
		}
		if err := ValidateEntityEndpointRoutes(crudMount, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD, States: ent.Config.States, Key: ent.KeyColumns()}, a.entityUI != nil && a.entityUIWouldOwn(ent), resolved, a.router.Routes()...); err != nil {
			return err
		}
	}

	claimed := map[string]bool{}
	if mcpTools {
		for _, name := range entityMCPToolNames(ent, m) {
			claimed[name] = true
		}
	}
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		if endpoint.MCP {
			if endpoint.MCPHandler == nil {
				return fmt.Errorf("endpoint %q: MCPHandler is required when MCP is true", endpoint.Path)
			}
			toolName := endpoint.Name
			if toolName == "" {
				_, toolPath := m.endpointPaths(endpoint)
				toolName = openapi.DefaultEndpointToolName(ent.GetName(), method, toolPath)
			}
			toolName = m.endpointTool(toolName)
			if claimed[toolName] {
				return fmt.Errorf("endpoint %q: MCP tool name %q is already claimed by this entity", endpoint.Path, toolName)
			}
			claimed[toolName] = true
		}
	}
	for toolName := range claimed {
		if a.MCP.HasTool(toolName) {
			return fmt.Errorf("MCP tool %q is already registered on the server: rename the entity, set a namespace, or rename the endpoint tool", toolName)
		}
	}
	return nil
}

func (a *App) registerEntityEndpoints(ent *entity.Entity, endpoints []entity.Endpoint) error {
	for _, endpoint := range endpoints {
		method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
		if method == "" {
			return fmt.Errorf("endpoint %q: method is required", endpoint.Path)
		}
		path := openapi.EntityEndpointRoutePath(ent, endpoint.Path, a.apiPrefix())
		specPath := openapi.EntityEndpointPath(ent, endpoint.Path)
		if endpoint.Handler != nil {
			crud.EntityRouter(a.router, ent).Handle(method, path, endpointRoute(endpoint))
		}
		if endpoint.MCP {
			if endpoint.MCPHandler == nil {
				return fmt.Errorf("endpoint %q: MCPHandler is required when MCP is true", endpoint.Path)
			}
			name := endpoint.Name
			if name == "" {
				name = openapi.DefaultEndpointToolName(ent.GetName(), method, specPath)
			}
			description := endpoint.Description
			if description == "" {
				description = method + " " + path
			}
			if err := a.MCP.RegisterTool(name, description, openapi.EndpointInputSchema(endpoint), endpointMCPTwin(path, endpoint.MCPHandler), endpointMCPOptions(ent, endpoint)...); err != nil {
				return err
			}
		}
	}
	return nil
}

func endpointRoute(ep entity.Endpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler.IsForgedCrossSiteWrite(r) {
			handler.WriteError(w, r, handler.Fail(http.StatusForbidden, handler.ReasonCrossSiteRefused))
			return
		}
		if !ep.Public {
			w.Header().Set("Cache-Control", "no-store")
			if _, ok := handler.GetUser(r.Context()); !ok {
				handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, handler.ReasonAuthenticationRequired))
				return
			}
		}
		ep.Handler.ServeHTTP(w, r)
	})
}

func endpointMCPOptions(ent *entity.Entity, ep entity.Endpoint) []mcp.ToolOption {
	var gate func(context.Context) error
	switch {
	case ep.MCPGate != nil:
		gate = ep.MCPGate
	case !ep.Public:
		gate = MCPRequireUser()
	}
	verb := "write"
	if m := strings.ToUpper(strings.TrimSpace(ep.Method)); m == http.MethodGet || m == http.MethodHead {
		verb = "read"
	}
	scope := access.Permission(ent.GetTable() + ":" + verb)
	return []mcp.ToolOption{mcp.WithToolGate(func(ctx context.Context) error {
		if gate != nil {
			if err := gate(ctx); err != nil {
				return err
			}
		}
		if !access.ScopeAllows(ctx, scope) {
			return errScopeNotHeld
		}
		return nil
	})}
}

func endpointMCPTwin(pattern string, h mcp.ToolHandler) mcp.ToolHandler {
	return mcp.Decorate(h, func(inner mcp.ToolHandler) mcp.ToolHandler {
		return func(ctx context.Context, params map[string]any) (any, error) {
			return inner(handler.WithRoutePattern(ctx, pattern), params)
		}
	})
}

// JSONCasing returns the configured JSON casing, CaseCamel by default.
func (a *App) JSONCasing() crud.JSONCase {
	if a.Config.JSONCase == "" {
		return crud.CaseCamel
	}
	return a.Config.JSONCase
}

// RegisterPlugin registers a plugin. It panics once InitPlugins has run
// or when the plugin manager refuses it.
func (a *App) RegisterPlugin(plugin Plugin) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterPlugin(%q) called after InitPlugins; register plugins before App.Start", plugin.Name()))
	}
	if err := a.Plugins.Register(plugin); err != nil {
		panic(fmt.Sprintf("framework: failed to register plugin %q: %v", plugin.Name(), err))
	}
	return a
}

// RegisterBattery registers a battery that initializes after the batteries
// named in deps. It panics once InitPlugins has run or when registration fails.
func (a *App) RegisterBattery(b Battery, deps ...string) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterBattery(%q) called after InitPlugins; register batteries before App.Start", b.Name()))
	}
	if err := a.Batteries.Register(b, deps...); err != nil {
		panic(fmt.Sprintf("framework: failed to register battery %q: %v", b.Name(), err))
	}
	return a
}

// InitPlugins runs Init on every plugin, in registration order, then on
// every battery, in dependency order. After a successful call, later
// calls return nil and run nothing.
func (a *App) InitPlugins() (err error) {
	if !a.initialized.CompareAndSwap(false, true) {
		return nil
	}
	defer func() {
		if err != nil {
			a.initialized.Store(false)
		}
	}()
	if err := a.Plugins.InitAll(a); err != nil {
		return err
	}
	if err := a.Batteries.InitAll(a); err != nil {
		return err
	}
	if err := a.modules.loadFromStore(context.Background()); err != nil {
		return err
	}
	a.reserveEmbedPrefixes()
	a.probeReadinessRegistrars()
	for _, register := range a.mcpRegistrars {
		if err := register(a.MCP); err != nil {
			return fmt.Errorf("register MCP tools: %w", err)
		}
	}

	if a.mcpIntrospection {
		if err := a.registerIntrospectionTools(); err != nil {
			if !a.mcpIntrospectionDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP introspection tools partially skipped", "error", err)
		}
		if err := a.registerContractTools(); err != nil {
			if !a.mcpIntrospectionDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP contract tools partially skipped", "error", err)
		}
		if dev.DevMCPEnabled() {
			if err := a.registerContractDevTools(); err != nil {
				a.Logger().Warn("dev MCP contract source tools partially skipped", "error", err)
			}
		}
	}
	if a.mcpControl {
		if err := a.registerControlTools(); err != nil {
			if !a.mcpControlDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP control tools partially skipped", "error", err)
		}
	}
	for _, appCfg := range a.mcpApps {
		if err := a.MCP.RegisterApp(appCfg); err != nil {
			return fmt.Errorf("framework: register MCP app %q: %w", appCfg.Name, err)
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

// Outbox returns the transactional event outbox, or nil without WithOutbox.
func (a *App) Outbox() *outbox.Outbox {
	return a.outbox
}

// HookRegistry returns (or creates) the hook registry for a named entity.
func (a *App) HookRegistry(entityName string) *hook.HookRegistry {
	a.hooksMu.Lock()
	defer a.hooksMu.Unlock()
	if a.hooks == nil {
		a.hooks = make(map[string]*hook.HookRegistry)
	}
	if _, ok := a.hooks[entityName]; !ok {
		reg := hook.NewHookRegistry()
		reg.SetLabel(entityName)
		a.hooks[entityName] = reg
	}
	return a.hooks[entityName]
}

// lookupHookRegistry is HookRegistry without the create: nil when none exists.
func (a *App) lookupHookRegistry(entityName string) *hook.HookRegistry {
	a.hooksMu.RLock()
	defer a.hooksMu.RUnlock()
	return a.hooks[entityName]
}

// OnStart registers fn to run once during boot, after plugin Init and
// before the host starts its transport, with the App's lifecycle context,
// which Shutdown cancels. Hooks run in registration order and the first
// error aborts the run. To stop the App from a hook, return an error:
// a Shutdown called from inside one never returns without a deadline.
func (a *App) OnStart(fn func(ctx context.Context) error) *App {
	a.startHooks = append(a.startHooks, fn)
	return a
}

// OnReady registers fn to run, in registration order, once the HTTP
// listener has bound, with its resolved address. It must not block. Run
// refuses an App with an OnReady hook under a host that binds no listener.
func (a *App) OnReady(fn func(addr string)) *App {
	a.readyHooks = append(a.readyHooks, fn)
	return a
}

// OnStop registers fn to run during Shutdown, after the host and the
// batteries have stopped, in reverse registration order.
func (a *App) OnStop(fn func() error) *App {
	a.lc.PrependDrainer(stopHookDrainer(fn))
	return a
}

// OnStopFirst registers a stop hook that runs after every OnStop hook,
// whenever either was registered.
func (a *App) OnStopFirst(fn func() error) *App {
	a.lc.AppendDrainer(stopHookDrainer(fn))
	return a
}

type stopHookDrainer func() error

func (f stopHookDrainer) Drain(_ context.Context) error { return f() }

// Lifecycle returns the App's graceful-shutdown coordinator.
func (a *App) Lifecycle() *lifecycle.Lifecycle { return a.lc }

// RunWithSignals waits for SIGINT or SIGTERM, then returns Shutdown's
// error, or nil when ctx ends first.
func (a *App) RunWithSignals(ctx context.Context) error {
	return a.lc.RunWithSignalsUsing(ctx, a.Shutdown)
}

// AddCron starts s with the App and stops it on Shutdown, waiting for
// running jobs until the drain deadline. Under RoleServe it does nothing.
func (a *App) AddCron(s *cron.Scheduler) *App {
	if !a.runsWorkers() {
		return a
	}
	if mod := a.modules.currentModule(); mod != "" {
		s.SetGate(a.modules.cronGate(mod))
	}
	a.OnStart(func(ctx context.Context) error {
		s.Start(ctx)
		return nil
	})
	a.lc.PrependDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		return s.StopContext(ctx)
	}))
	return a
}

func (a *App) runStartHooks() error {
	a.ensureLifecycleContext()
	if err := a.Batteries.StartAll(a.appCtx); err != nil {
		return err
	}
	if a.stopRequestedNow() {
		return context.Canceled
	}
	if a.processModules != nil {
		a.processModules.StartLoops()
	}
	for _, fn := range a.startHooks {
		if a.stopRequestedNow() {
			return context.Canceled
		}
		if err := a.runStartHookSafe(fn); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) runStartHookSafe(fn func(ctx context.Context) error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(a.Logger(), "start hook", fmt.Sprintf("%T", v))
		}
	}()
	return fn(a.appCtx)
}

type schedulerStartStop interface {
	Start(ctx context.Context)
	Close() error
}

// AddQueue starts q with the App and closes it on Shutdown, giving up the
// wait at the drain deadline. Under RoleServe it does nothing.
func (a *App) AddQueue(q schedulerStartStop) *App {
	if !a.runsWorkers() {
		return a
	}
	if mod := a.modules.currentModule(); mod != "" {
		if g, ok := q.(interface{ SetGate(func(string) bool) }); ok {
			g.SetGate(a.modules.queueGate(mod))
		}
	}
	a.OnStart(func(ctx context.Context) error {
		q.Start(ctx)
		return nil
	})
	a.lc.PrependDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		return drainQueueClose(ctx, q)
	}))
	return a
}

func drainQueueClose(ctx context.Context, q schedulerStartStop) error {
	done := make(chan error, 1)
	go func() { done <- q.Close() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("queue Close abandoned after the drain deadline (a job handler ignored its cancelled context): %w", ctx.Err())
	}
}

func (a *App) trackConnState(c net.Conn, st http.ConnState) {
	a.newConnsMu.Lock()
	defer a.newConnsMu.Unlock()
	if st == http.StateNew {
		if a.newConns == nil {
			a.newConns = make(map[net.Conn]struct{})
		}
		a.newConns[c] = struct{}{}
		return
	}
	delete(a.newConns, c)
}

func (a *App) closeNewConns() {
	a.newConnsMu.Lock()
	conns := make([]net.Conn, 0, len(a.newConns))
	for c := range a.newConns {
		conns = append(conns, c)
	}
	a.newConnsMu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Shutdown turns /readyz to 503, keeps serving for the readiness grace,
// then stops the host, the batteries in reverse dependency order and the
// OnStop hooks. The teardown runs once: a later or concurrent call waits
// for it and returns nil, or ctx's error when ctx ends first. Called
// during boot it cancels the run, which tears down once its current phase
// returns. Called from inside a boot callback it returns only at ctx's
// deadline; return an error from the callback instead.
func (a *App) Shutdown(ctx context.Context) error {
	_, err := a.stop(ctx)
	return err
}

// stop is Shutdown; began reports whether this call ran the teardown.
func (a *App) stop(ctx context.Context) (began bool, err error) {
	a.shutdownMu.Lock()
	if done := a.shutdownDone; done != nil {
		a.shutdownMu.Unlock()
		select {
		case <-done:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	done := make(chan struct{})
	a.shutdownDone = done
	a.serverMu.Lock()
	a.stopRequested = true
	handoff := a.booting || a.deferredStarting
	a.teardownToEngine = handoff
	if handoff {
		a.handoffCtx = ctx
	}
	cancel := a.appCancel
	a.serverMu.Unlock()
	a.shutdownMu.Unlock()
	a.draining.Store(true)
	if handoff {
		if cancel != nil {
			cancel()
		}
		select {
		case <-done:
			return true, a.handoffErr
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
	defer close(done)
	return true, a.shutdown(ctx)
}

func (a *App) shutdown(ctx context.Context) error {
	a.drainReadiness(ctx)
	var firstErr error
	a.serverMu.Lock()
	cancel := a.appCancel
	a.appCancel = nil
	a.server = nil
	host := a.runHost
	a2aSrv := a.a2a
	a.serverMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if a2aSrv != nil {
		if err := a2aSrv.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("stopping a2a runs: %w", err)
		}
	}
	if host != nil {
		if err := a.stopHost(ctx, host); err != nil {
			firstErr = err
		}
	}
	if a.processModules != nil {
		_ = a.processModules.Drain(ctx)
	}
	if err := a.Batteries.StopAll(ctx); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("stopping batteries: %w", err)
	}
	if err := a.lc.Shutdown(ctx); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("running stop hooks: %w", err)
	}
	return firstErr
}

func (a *App) migrateAndSeed() error {
	if a.DB == nil {
		return nil
	}
	if !a.noAutoMigrate {
		if err := migrate.AutoMigratePlanContext(a.appCtx, a.DB, a.MigrationPlan()); err != nil {
			return fmt.Errorf("auto-migrate: %w", err)
		}
	}
	if err := migrate.RunSeeds(a.appCtx, a.DB, a.Registry); err != nil {
		return fmt.Errorf("run seeds: %w", err)
	}
	return nil
}

func (a *App) schemaSize() int {
	return a.Registry.size() + len(a.migrationViews) + len(a.migrationRoutines)
}

func (a *App) runSeedHooksSerialized() error {
	a.ensureLifecycleContext()
	if a.DB == nil {
		return a.runSeedHooks()
	}
	dialect, err := a.txDialect()
	if err != nil {
		return fmt.Errorf("seed hooks: %w", err)
	}
	if dialect != migrate.DialectPostgres {
		return a.runSeedHooks()
	}
	if a.DB.Stats().MaxOpenConnections == 1 {
		a.Logger().Warn("seed hooks advisory lock skipped: Postgres pool has MaxOpenConns(1), so App.WithSeed hooks are NOT serialized across replicas: raise MaxOpenConns above 1")
		return a.runSeedHooks()
	}
	return coremig.WithAdvisoryLockKey(
		a.appCtx, a.DB, migrate.DialectPostgres, coremig.SeedAdvisoryLockKey,
		func(_ *sql.Conn) error { return a.runSeedHooks() },
	)
}

func (a *App) ensureLifecycleContext() {
	a.serverMu.Lock()
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}
	a.serverMu.Unlock()
}

func (a *App) warnUnresolvableRelations() {
	if a.Registry == nil {
		return
	}
	log := a.Logger()
	for _, ent := range a.Registry.AllSorted() {
		for _, rel := range ent.Config.Relations {
			if _, err := entity.ResolveTarget(a.Registry, ent, rel.Entity); err == nil {
				continue
			}
			log.Warn("relation target is not a registered entity; ?include= on it will be refused",
				"entity", ent.GetName(),
				"relation", rel.Name,
				"target", rel.Entity)
		}
	}
}

// Start is Run with the built-in HTTP host on addr, which worktree
// isolation may remap. It returns nil after Shutdown, and handles SIGINT
// and SIGTERM unless AppConfig.DisableSignalHandling is set.
func (a *App) Start(addr string) error {
	return a.run(context.Background(), &httpHost{app: a, addr: addr})
}

// Serve is Start on a listener the caller owns, which isolation never
// remaps. OnReady receives ln.Addr().String(), and Serve closes ln when
// it returns, on every path.
func (a *App) Serve(ln net.Listener) error {
	if ln == nil {
		return errors.New("framework: Serve: nil listener")
	}
	l := &onceCloseListener{Listener: ln}
	defer func() { _ = l.Close() }()
	return a.run(context.Background(), &httpHost{app: a, ln: l})
}

type onceCloseListener struct {
	net.Listener
	once sync.Once
	err  error
}

func (l *onceCloseListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	return l.err
}

func inProcessNetwork(network string) bool {
	return network == "memory" || network == "pipe"
}

func tcpNetwork(network string) bool {
	return network == "tcp" || network == "tcp4" || network == "tcp6"
}

func (a *App) resolveListenAddr(addrp *string) error {
	addr := *addrp
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		return fmt.Errorf("resolve isolation: %w", err)
	}
	requestedAddr := addr
	addr, err = runtimeIsolation.Addr(addr)
	if err != nil {
		return fmt.Errorf("resolve isolated addr: %w", err)
	}
	if runtimeIsolation.Active() && requestedAddr != "" && addr != requestedAddr {
		a.Logger().Warn("isolation remapped the listen address",
			"requested", requestedAddr,
			"resolved", addr,
			"isolation", runtimeIsolation.ID(),
			"disable", "GOFASTR_ISOLATION=off (or GOFASTR_ISOLATION_REWRITE=0 for explicit values only)")
	}
	*addrp = addr
	return nil
}

func (a *App) bootSetup(web bool) (interactive bool, err error) {
	if a.setup == nil {
		return false, nil
	}
	mode, err := resolveSetupEnv()
	if err != nil {
		return false, fmt.Errorf("setup: %w", err)
	}
	if mode == setupOff {
		return false, nil
	}
	incomplete, err := a.setup.Incomplete(a.appCtx)
	if err != nil {
		return false, fmt.Errorf("setup: %w", err)
	}
	if mode != setupForce && !incomplete {
		return false, nil
	}
	if a.role == RoleWorker {
		return false, fmt.Errorf("setup incomplete: run a serve/all process to complete setup first")
	}
	canHeadless, err := a.setup.CanRunHeadless(a.appCtx)
	if err != nil {
		return false, fmt.Errorf("setup: %w", err)
	}
	if canHeadless {
		if err := a.setup.RunSteps(a.appCtx); err != nil {
			return false, fmt.Errorf("setup: %w", err)
		}
		return false, nil
	}
	if !web {
		return false, errors.New("setup incomplete: the interactive setup wizard needs an HTTP host; supply every required setup value so setup runs headless, or complete setup through Start once")
	}
	return true, nil
}

func (a *App) finalizeRoutes() (hasAPI, hasLLMMD bool, err error) {
	hasAPI = len(a.Registry.All()) > 0
	if hasAPI {
		appName := a.Config.Name
		if appName == "" {
			appName = "GoFastr API"
		}
		spec := a.OpenAPISpec(appName)
		if a.Config.PublicOpenAPI {
			a.router.Get("/openapi.json", coreoa.PublicHandler(spec))
		} else {
			a.router.Get("/openapi.json", coreoa.Handler(spec))
		}
		a.router.Get("/api/docs/", coreoa.DocsHandler(spec, "/api/docs", a.Config.PublicOpenAPI))
		if !a.Config.NoLLMMD {
			pagesIndex := a.pageIndexPath()
			if a.Config.PublicOpenAPI {
				doc := []byte(crud.RegistryLLMMD(a.Registry, appName, a.entityCrudMount, pagesIndex))
				a.router.Get("/api/llm.md", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Cache-Control", "no-store")
					w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
					w.Header().Set("Content-Length", fmt.Sprintf("%d", len(doc)))
					w.Write(doc)
				}))
			} else {
				a.router.Get("/api/llm.md", crud.RegistryLLMMDHandler(a.Registry, appName, a.entityCrudMount, pagesIndex))
			}
			hasLLMMD = true
		}
	}
	if a.metrics != nil {
		a.router.Get("/metrics", middleware.MetricsHandler(a.Metrics()))
	}

	if a.Config.DebugEndpoints {
		a.registerDebugEndpoints()
	}
	if a.Config.Name != "" {
		a.MCP.SetServerName(a.Config.Name)
	}
	if a.mcpAutoMount {
		if a.mcpMountDevImplied && a.routerHasMCPRoute() {
			a.Logger().Warn("dev MCP auto-mount skipped: /mcp is already mounted by the host")
		} else {
			h := a.MCP.ServeSSE("/mcp")
			a.router.Post("/mcp", h)
			a.router.Get("/mcp", h)
		}
	}
	if len(a.mcpApps) > 0 && !a.mcpAutoMount && !a.routerHasMCPRoute() {
		a.Logger().Warn("WithMCPApp registered but /mcp is not mounted: the widget and its tool will be unreachable; add framework.WithMCP()")
	}
	if a.DB != nil && !a.mcpAutoMount && !a.routerHasMCPRoute() {
		for _, e := range a.Registry.All() {
			if e.Config.Exposure != nil && e.Config.Exposure.MCP {
				a.Logger().Warn("entity MCP tools are registered but /mcp is not mounted: they will be unreachable; add framework.WithMCP()")
				break
			}
		}
	}
	if a.a2aCfg != nil {
		if err := a.mountA2A(); err != nil {
			return hasAPI, hasLLMMD, err
		}
	}
	if a.mcpAutoMount {
		a.router.Get("/mcp/server-card", http.HandlerFunc(a.handleMCPServerCard))
		a.router.Get("/.well-known/mcp/catalog.json", http.HandlerFunc(a.handleMCPCatalog))
	}
	if a.oauthResource != nil {
		for _, p := range a.oauthResource.metadataPaths() {
			a.router.Get(p, http.HandlerFunc(a.handleOAuthProtectedResource))
		}
	}
	if hasAPI {
		a.router.Get("/.well-known/api-catalog", http.HandlerFunc(a.handleAPICatalog))
	}
	if a.mcpAutoMount {
		a.router.Get("/.well-known/mcp/server-card.json", http.HandlerFunc(a.handleMCPServerCard))
		a.router.Get("/.well-known/mcp.json", http.HandlerFunc(a.handleMCPManifest))
		for _, mountable := range a.mountables {
			if discovery, ok := mountable.(interface{ SetMCPManifestPath(string) }); ok {
				discovery.SetMCPManifestPath("/.well-known/mcp.json")
			}
		}
	}
	a.router.Get("/.well-known/agent-skills/index.json", http.HandlerFunc(a.handleAgentSkillsIndex))
	if a.oauthAuthServer != nil {
		a.router.Get("/.well-known/oauth-authorization-server", http.HandlerFunc(a.handleOAuthAuthorizationServer))
	}
	if a.authMD != nil {
		a.router.Get("/auth.md", http.HandlerFunc(a.handleAuthMD))
	}
	if a.webBotAuth != nil {
		a.router.Get("/.well-known/http-message-signatures-directory", http.HandlerFunc(a.handleWebBotAuthDirectory))
	}
	if a.ucp != nil {
		a.router.Get("/.well-known/ucp", http.HandlerFunc(a.handleUCP))
	}
	if a.acp != nil {
		a.router.Get("/.well-known/acp.json", http.HandlerFunc(a.handleACP))
	}
	a.installAPIMissProblems()
	if a.DB != nil {
		c := dbReadinessCheck(a)
		a.RegisterReadiness(c.Name, c.Check)
	}
	a.registerHealthEndpoints()
	a.checkLocaleRoutes()
	for _, m := range a.mountables {
		if bv, ok := m.(BootValidator); ok {
			bv.ValidateBoot()
		}
	}

	return hasAPI, hasLLMMD, nil
}

func (a *App) runReadyHookSafe(fn func(addr string), addr string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(a.Logger(), "ready hook", fmt.Sprintf("%T", v))
		}
	}()
	fn(addr)
	return nil
}

func (a *App) printStartupBanner(boundAddr, network, name string, hasAPI, hasLLMMD bool, setupURL string) {
	base := a.urlScheme() + "://" + boundAddr
	listening := base
	if _, _, err := net.SplitHostPort(boundAddr); err != nil || !tcpNetwork(network) {
		base, listening = "", boundAddr+" (not a TCP address)"
		setupURL = strings.TrimPrefix(setupURL, a.urlScheme()+"://"+boundAddr)
	}
	w := a.startupOutput
	if w == nil {
		w = os.Stdout
	}
	if a.role == RoleWorker {
		fmt.Fprintf(w, "\n  %s %s worker ready\n", bold("GoFastr"), name)
		fmt.Fprintf(w, "  %s PID: %d\n", arrow(), os.Getpid())
		fmt.Fprintf(w, "  %s Health: %s/healthz %s/readyz\n", arrow(), base, base)
		_, _ = fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "\n  %s %s server ready\n", bold("GoFastr"), name)
	fmt.Fprintf(w, "  %s PID: %d\n", arrow(), os.Getpid())
	fmt.Fprintf(w, "  %s Listening: %s\n", arrow(), listening)
	if setupURL != "" {
		fmt.Fprintf(w, "  %s Setup: %s\n", arrow(), setupURL)
	}
	if a.Config.DebugEndpoints {
		fmt.Fprintf(w, "  %s Stats: %s/.debug/stats\n", arrow(), base)
	}

	skipped, gated := 0, 0
	for _, e := range a.Registry.AllSorted() {
		if !a.entityCRUDEnabled(e) {
			skipped++
			continue
		}
		mountPath := "/" + e.GetTable()
		if e.Version != "" {
			mountPath = e.Version + mountPath
		} else {
			mountPath = a.entityMountPath(e.GetTable())
		}
		label := e.GetName()
		if e.Version != "" {
			label = e.GetName() + " (" + e.Version + ")"
		}
		gate := "  (requires auth)"
		if e.Config.Exposure != nil && e.Config.Exposure.Public {
			gate = ""
		} else {
			gated++
		}
		fmt.Fprintf(w, "  %s %-12s %s%s%s\n", arrow(), label, base, mountPath, gate)
	}
	if skipped > 0 {
		noun := "entities"
		if skipped == 1 {
			noun = "entity"
		}
		fmt.Fprintf(w, "  %s %d more %s registered without CRUD routes\n", arrow(), skipped, noun)
	}
	if gated > 0 && !a.authenticatorNoted.Load() {
		fmt.Fprintf(w, "  %s No auth is wired: anonymous requests to the URLs above answer 401. Wire battery/auth, or set Exposure.Public on an entity anyone may read.\n", arrow())
	}

	if hasAPI {
		apiGate := "  (requires auth: WithPublicOpenAPI() to expose)"
		if a.Config.PublicOpenAPI {
			apiGate = ""
		}
		fmt.Fprintf(w, "  %s OpenAPI:     %s/openapi.json%s\n", arrow(), base, apiGate)
		fmt.Fprintf(w, "  %s API docs:    %s/api/docs/%s\n", arrow(), base, apiGate)
		if hasLLMMD {
			fmt.Fprintf(w, "  %s LLM Docs:    %s/api/llm.md%s\n", arrow(), base, apiGate)
		}
	}

	if a.mcpAutoMount && a.MCP != nil {
		mcpNote := ""
		if n := len(a.MCP.ListTools()); n > 0 {
			mcpNote = fmt.Sprintf("  (%d tools, your app's permissions)", n)
		}
		fmt.Fprintf(w, "  %s MCP:         %s/mcp%s\n", arrow(), base, mcpNote)
	}
	_, _ = fmt.Fprintln(w)
}

// NoteAuthenticator records that something on this app signs callers in.
// Until it is called, the startup banner says auth-gated URLs answer 401.
func (a *App) NoteAuthenticator() { a.authenticatorNoted.Store(true) }

// AuthenticatorNoted reports whether NoteAuthenticator has been called.
func (a *App) AuthenticatorNoted() bool { return a.authenticatorNoted.Load() }

func (a *App) debugAllowed(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if u, ok := handler.GetUser(ctx); !ok || u == nil {
		i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
		return false
	}
	allowed := false
	if _, embedded := fembed.GrantFromContext(ctx); !embedded && !deciderDenies(ctx) {
		if a.debugAuthorize != nil {
			allowed = access.ScopeAllows(ctx, access.RoleScope("admin")) && a.debugAuthorize(ctx)
		} else {
			allowed = hasDebugAdminRole(ctx)
		}
	}
	if !allowed {
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
	}
	return allowed
}

func hasDebugAdminRole(ctx context.Context) bool {
	return slices.Contains(callerRoles(ctx), "admin")
}

func (a *App) registerDebugEndpoints() {
	a.router.Get("/.debug/stats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !a.debugAllowed(w, r) {
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
			"entities":       len(a.Registry.All()),
			"jsonCase":       string(a.JSONCasing()),
			"goroutineLeaks": goroutineLeakCount(),
		}

		b, err := json.Marshal(stats)
		if err != nil {
			slog.Warn("debug stats: encode", "error", err)
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
		w.Write([]byte("\n"))
	}))
	a.router.Get("/.debug/goroutineleak", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !a.debugAllowed(w, r) {
			return
		}
		p := pprof.Lookup("goroutineleak")
		if p == nil {
			i18n.HTTPError(w, r, http.StatusNotImplemented, i18n.StatusPhrase(http.StatusNotImplemented))
			return
		}
		runtime.GC()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = p.WriteTo(w, 1)
	}))
}

func goroutineLeakCount() int {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return -1
	}
	runtime.GC()
	return p.Count()
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
