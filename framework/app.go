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
	// WithJSONCase, not a field assignment: NewCrudHandler has already
	// built the wire-key snapshot under the default case, and only
	// WithJSONCase republishes it. Assigning the field left a snake_case
	// app answering camelCase keys.
	ch := crud.NewCrudHandler(ent, a.DB).WithJSONCase(a.JSONCasing())
	ch.Hooks = a.HookRegistry(name)
	ch.ChildHooks = a.lookupHookRegistry
	ch.Storage = a.Storage
	ch.ImageDeriver = a.imageDeriver
	ch.FieldImageDerivers = a.fieldImageDerivers[name]
	ch.StripUploadMetadata = a.stripUploadMetadata
	ch.Events = a.Events()
	// Guarded assignment: a bare `= a.outbox` would wrap a typed nil in the
	// EventOutbox interface and silently swallow every event.
	if a.outbox != nil {
		ch.Outbox = a.outbox
	}
	ch.Registry = a.Registry
	return ch
}

// CrudHandlerForEntity is CrudHandler that takes the entity directly instead
// of looking it up by name. Use it when you already hold the *entity.Entity
// (e.g. from Registry.All or AllSorted) and the name may be ambiguous because
// multiple versions are registered. The handler is wired identically to the
// one CrudHandler(name) produces.
func (a *App) CrudHandlerForEntity(ent *entity.Entity) (*crud.CrudHandler, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: no DB configured")
	}
	if ent == nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: entity is nil")
	}
	// The entity must be the one the registry holds at that (name, version).
	// Building a handler for an unregistered entity yields something wired to
	// routes that do not exist. Every dispatch through it 404s, far from the
	// call that made it.
	registered, err := a.Registry.GetVersioned(ent.Config.Name, ent.Version)
	if err != nil {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: %w", err)
	}
	if registered != ent {
		return nil, fmt.Errorf("app.CrudHandlerForEntity: entity %q at version %q is not the registered instance", ent.Config.Name, ent.Version)
	}
	ch := a.wireCrudHandler(ent)
	// BasePath is where the entity's routes are actually mounted. mcpBase() is
	// BasePath + "/" + table, so leaving it empty addresses "/posts" while a
	// versioned entity's routes live at "/api/v1/posts". An unversioned entity
	// takes the app-wide API prefix, matching App.Entity.
	if ent.Version != "" {
		ch.BasePath = ent.Version
	} else {
		ch.BasePath = a.apiPrefix()
	}
	return ch, nil
}

// MustCrudHandler is CrudHandler that panics on error, for app setup where a
// missing entity is a programming mistake.
func (a *App) MustCrudHandler(name string) *crud.CrudHandler {
	ch, err := a.CrudHandler(name)
	if err != nil {
		panic(err)
	}
	return ch
}

// Table registers a raw, non-entity table for migration only, no CRUD, no
// HTTP routes, no validation, no auto-injected columns. The table participates
// in auto-migrate, diffing, and generation alongside entities (including
// foreign keys that cross between the two). For users who want migration
// coverage of a table without the entity machinery. Returns App for chaining.
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

// Routine registers a stored routine (function, procedure, trigger, or view)
// as a first-class migration object. Its Up runs on every boot (idempotent
// CREATE OR REPLACE) after tables are migrated, and `migrate generate` tracks
// it for reversible versioned migrations. Returns App for chaining.
func (a *App) Routine(r migrate.Routine) *App {
	a.migrationRoutines = append(a.migrationRoutines, r)
	return a
}

// RoutinesFS loads routines authored as embedded SQL files via
// migrate.RoutinesFS and registers each one as if App.Routine had been called
// on it. This is the primary authoring path for stored procedures: an app
// writes `db/routines/compute_totals.pg.sql` containing
// `CREATE OR REPLACE FUNCTION …` and calls
// `app.RoutinesFS(embeddedFS, "db/routines")`. See migrate.RoutinesFS for the
// filename grammar (`<name>.sql`, `<name>.down.sql`, `<name>.pg.sql`,
// `<name>.sqlite.sql`) and the loud-rejection rules (empty file, empty dir,
// plain+dialect Up collision). A loader error panics at registration time,
// mirroring App.View's misconfig panic, so the exact embed-path/file error
// surfaces in the console instead of shipping a half-loaded routine set.
func (a *App) RoutinesFS(fsys fs.FS, dir string) *App {
	rs, err := migrate.RoutinesFS(fsys, dir)
	if err != nil {
		panic(fmt.Sprintf("framework: App.RoutinesFS(%q): %v", dir, err))
	}
	a.migrationRoutines = append(a.migrationRoutines, rs...)
	return a
}

// View registers a database view, a virtual table built from other entities.
// The view is created on boot after its source tables (and tracked reversibly
// by `migrate generate`), and, when it declares Columns, it is also exposed
// through the ORM as a READ-ONLY entity: List/Get and the query layer work, but
// no write routes are registered. Returns App for chaining.
func (a *App) View(v migrate.View) *App {
	a.migrationViews = append(a.migrationViews, v)

	ent := v.ToEntity() // nil when no columns (migration-only); panics if Columns lack a PrimaryKey
	if ent == nil {
		return a
	}
	// Its read routes name rows by id, and a view skips Define's key check.
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
		ch := crud.NewCrudHandler(ent, a.DB).WithJSONCase(a.JSONCasing()) // republishes the wire keys; see wireCrudHandler
		ch.Registry = a.Registry
		ch.ChildHooks = a.lookupHookRegistry
		ch.Events = a.Events()
		// Views mount under the API prefix like entities do, so BasePath must
		// match or anything deriving a URL from the handler addresses the
		// wrong path. mcpBase() is BasePath + "/" + table, so an empty value
		// silently yields "/posts" where the routes live at "/api/posts", a
		// 404 surfacing far from its cause the moment a view gains a tool.
		ch.BasePath = a.apiPrefix()
		crud.RegisterCrudRoutes(a.router, ch, a.entityMountPath(ent.GetTable()),
			crud.CrudRouteOptions{ReadOnly: true, NoLLMMD: a.Config.NoLLMMD})
	}
	return a
}

// MigrationPlan returns the full migration Plan Start applies on boot, the
// entity registry plus every routine and view registered via App.Routine /
// App.RoutinesFS / App.View. It is the schema source for the host-binary
// generation path: pass it to migrate.GenerateMigrationFile so a `myapp
// migrate generate <name>` verb emits versioned migrations from the same
// compiled schema boot auto-migrates, with no app spec and no database
// touched. See the "Generating from a host binary" section of migrations.md.
func (a *App) MigrationPlan() migrate.Plan {
	return migrate.Plan{Registry: a.Registry, Views: a.migrationViews, Routines: a.migrationRoutines}
}

// RegisterEntities registers each (name, config) pair via App.Entity in
// alphabetical-by-name order. Sorting matters: Entity has order-sensitive
// side effects, router registration, MCP tool list order, OpenAPI tag
// emission, and Go's map iteration is randomised, so unsorted iteration
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

// entityRouteCollision reports an actionable diagnostic when the entity
// named `name` would mount CRUD routes at a path already owned by another
// registration (typically a UI screen registered at the same name). It
// returns "" when there is no collision.
//
// The auto-CRUD mount registers GET <mountPath>, GET <mountPath>/{id},
// GET <mountPath>/llm.md, etc. Without this check the first overlap panics
// deep in net/http's ServeMux, usually on /llm.md, with a message that
// points at the generated doc handler rather than the underlying name
// clash. We catch the most common overlaps (the bare path and its /llm.md
// doc route) and explain WHAT collided and HOW to fix it.
func (a *App) entityRouteCollision(ent *entity.Entity, mountPath string) string {
	name := ent.Config.Name
	mountPath = strings.TrimRight(mountPath, "/")
	if mountPath == "" {
		return ""
	}
	// Ask crud for the full set it will claim rather than guessing at it.
	// An earlier version listed only the bare path and /llm.md, on the
	// theory that {id}/_batch/_events were "entity-only territory", but a
	// detail page at /posts/{id} is the framework's own suggested idiom
	// (see entityScreenCollisionMessage), and a screen there collided in
	// the commit phase, after the registry entry and the first CRUD routes
	// were already published and with no way to un-publish them.
	// Keyed by normalized path, holding the methods CRUD will claim. A
	// route registered without a method is treated as claiming all of
	// them, better a loud false positive at registration than a panic
	// halfway through the commit phase.
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
	// An equal shape is not the only conflict: ServeMux also refuses two
	// patterns that overlap with neither more specific, such as GET
	// /{kind}/_events against the generated GET /posts/{id}, and that
	// panic came mid-commit (2026-10-27 S7). Every route goes through the
	// router's one registration point and is recorded there, so replaying
	// them into a scratch mux asks the real mux's question.
	held := newMuxReplay(routes)
	for _, pattern := range patterns {
		if conflict, refused := held.add(pattern, "this entity's own generated CRUD route"); refused {
			return entityScreenCollisionMessage(name, mountPath, conflict.pattern)
		}
	}
	return ""
}

// muxReplay is a scratch ServeMux holding the router's registered routes
// and any patterns added since, each with what owns it. Every route goes
// through the router's one registration point and is recorded there, so
// the scratch mux refuses exactly what the router's mux would, overlaps
// included.
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

// add registers pattern under owner. When the mux refuses it, add
// returns the held pattern it conflicts with; a conflict only the whole
// set produces names no single pattern.
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

// muxAccepts registers pattern on mux with a no-op handler and reports
// whether the mux took it rather than panicking on a conflict.
func muxAccepts(mux *http.ServeMux, pattern string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	mux.Handle(pattern, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	return true
}

// normalizeRoutePattern rewrites every wildcard segment to a constant, so
// two patterns that net/http's ServeMux considers equivalent compare equal
// here too.
//
// ServeMux conflicts on the SHAPE of a pattern, not the wildcard's name:
// "GET /posts/{slug}" and the generated "GET /posts/{id}" match the same
// requests and cannot both be registered. Comparing the raw strings misses
// that and lets the clash through to a mid-commit panic.
func normalizeRoutePattern(pattern string) string {
	segments := strings.Split(pattern, "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			// A trailing {x...} matches multiple segments and is a
			// different shape from a single-segment wildcard.
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

// validateEntityRegistration runs every configuration check that could
// reject an entity declaration WITHOUT touching the registry, router, or
// MCP server. TryEntity calls it before its commit phase; keep its rules
// in lockstep with registerEntityEndpoints and crud.RegisterEntityMCPTools.
// A check that only exists at commit time reintroduces the partial
// registration this split exists to prevent.
func (a *App) validateEntityRegistration(ent *entity.Entity, endpoints []entity.Endpoint, mcpTools bool, crudMount string, m registrationMount) error {
	if err := validateDisplayQueries(ent); err != nil {
		return err
	}
	// An entity registered after EntityUI gets the checks EntityUI ran
	// on the ones before it.
	if a.entityUI != nil {
		if err := a.entityUI.CheckEntity(ent); err != nil {
			return err
		}
	}
	// A queryable field whose name is another's plus an operator suffix
	// (`status` beside `status_ne`) registers: the bare key ?status_ne=
	// reads two ways and every filter parser refuses it with the bracket
	// spellings that pick one (?status_ne[eq]=, ?status[ne]=), so neither
	// column is unreachable and no reading is guessed (filter.ReadKey).
	// Endpoint routes: an endpoint whose (method, path) is already taken,
	// by an existing route, by a CRUD route this same call is about to
	// mount, or by a sibling endpoint on this same declaration, would
	// panic inside router.Handle during the commit phase, i.e. AFTER the
	// registry entry and CRUD routes are already published. Recovering
	// that panic into an error cannot un-publish them, so the collision
	// has to be caught here.
	if len(endpoints) > 0 || (a.entityUI != nil && crudMount != "" && a.entityUIWouldOwn(ent)) {
		resolved := make([]Endpoint, 0, len(endpoints))
		for _, ep := range endpoints {
			if strings.TrimSpace(ep.Method) == "" || ep.Handler == nil {
				continue // shape errors are reported below
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
		// The flat "<entity>_<action>" tool names are a documented-stable
		// public surface (see crud.RegisterEntityMCPTools), which is what
		// lets us pre-compute them here without reaching into crud.
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
		// path is where the endpoint is mounted (prefix applied for relative
		// paths); specPath is the prefix-relative form the OpenAPI spec uses.
		// The auto-generated tool name is derived from specPath so it keeps
		// matching the spec's operationId, the prefix moves the route, not
		// the endpoint's identity.
		path := openapi.EntityEndpointRoutePath(ent, endpoint.Path, a.apiPrefix())
		specPath := openapi.EntityEndpointPath(ent, endpoint.Path)
		if endpoint.Handler != nil {
			// Named by the entity's table, the scope its MCP twin checks
			// (endpointMCPOptions), so RequireAPIScopes asks the same.
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

// endpointRoute is an Endpoint's Handler behind the gates every endpoint
// route carries, the ones the CRUD routes apply to their own: a forgeable
// cross-site request answers 403 on every method but GET and HEAD, and an
// anonymous caller answers 401 unless the endpoint is Public. Before this,
// Handler ran with whatever the app's middleware chain happened to hold,
// so an endpoint on an entity whose CRUD answered 401 served anyone.
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

// endpointMCPOptions returns the registration options for an Endpoint's MCP
// twin. An Endpoint's two front doors do not get the same protection for
// free: endpointRoute gates the HTTP route, MCPHandler is registered
// straight onto the MCP server and sees none of it. So the twin defaults to
// requiring an authenticated caller, and an endpoint that really is
// anonymous says so with Public.
//
// A scope-restricted caller (an API token, access.WithHeldScopes) must also
// hold "<table>:<verb>", GET and HEAD reading and every other method
// writing: the scope auth.RequireAPIScopes demands of the REST route. This
// holds under MCPGate and Public too, as RequireAPIScopes holds on a Public
// route, since scopes only narrow what a credential may reach.
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

// endpointMCPTwin wraps an Endpoint's MCPHandler so the tool's call
// context carries the HTTP route pattern it is the twin of. The twin's
// path parameters are then sanitized with the route's own catch-all
// flags (handler.WithRoutePattern), so a {path...} route's tool accepts
// a slash-bearing value the route accepts, dot-dot and empty segments
// and control bytes still refused. A tool registered by hand keeps
// every path parameter strict.
//
// mcp.Decorate keeps an mcp.Gated MCPHandler's gates outside the twin,
// where registration finds them: the server then runs them before
// argument validation, so a refused caller gets the unknown-tool answer
// whatever arguments it sends (#669).
func endpointMCPTwin(pattern string, h mcp.ToolHandler) mcp.ToolHandler {
	return mcp.Decorate(h, func(inner mcp.ToolHandler) mcp.ToolHandler {
		return func(ctx context.Context, params map[string]any) (any, error) {
			return inner(handler.WithRoutePattern(ctx, pattern), params)
		}
	})
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
// Panics if InitPlugins has already run. Plugins must be registered
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
// needs from inside Init, register routes, add middleware, register MCP
// tools, attach hooks, swap the logger, etc.
//
// Idempotent: the first successful call latches an internal flag so any
// later call returns nil without re-running plugin Inits. This lets tests
// call InitPlugins() manually pre-Start without colliding with the
// implicit call inside Start.
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
	// Load module enable/disable state from the store into the in-memory
	// cache. Modules absent from the store default to enabled.
	if err := a.modules.loadFromStore(context.Background()); err != nil {
		return err
	}
	// Now that every plugin and battery has run Init and settled its own
	// prefix, tell the embed host which paths a grant must never reach.
	// Panics on a conflict. A surface declared over a battery's routes is a
	// wiring mistake that must not boot.
	a.reserveEmbedPrefixes()

	// Probe both plugins and batteries for the optional
	// ReadinessRegistrar interface so they can publish health checks
	// before /readyz mounts in Start.
	a.probeReadinessRegistrars()

	// Host-supplied tool registrars (WithMCPTools) run before the
	// introspection set so a dev-implied introspection tool yields to a
	// host tool of the same name instead of the reverse.
	for _, register := range a.mcpRegistrars {
		if err := register(a.MCP); err != nil {
			return fmt.Errorf("register MCP tools: %w", err)
		}
	}

	// Register introspection MCP tools if opted in. After plugin/battery
	// init so app_plugins / app_batteries reflect everything. Dev-implied
	// registration (see NewApp) downgrades collisions with host-registered
	// tool names to a warning, dev must never fail an app that boots fine
	// in production.
	if a.mcpIntrospection {
		if err := a.registerIntrospectionTools(); err != nil {
			if !a.mcpIntrospectionDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP introspection tools partially skipped", "error", err)
		}
		// The contract catalog rides along with introspection: both answer
		// "what shape is this app expected to have", and an agent that can
		// see the routes should also be able to see the rules those routes
		// are held to.
		if err := a.registerContractTools(); err != nil {
			if !a.mcpIntrospectionDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP contract tools partially skipped", "error", err)
		}
		// contracts_verify / contracts_fix read (and fix WRITES) the source
		// tree, so they ride the dev-loop gate rather than introspection
		// itself: an explicit WithMCPIntrospection() in production must not
		// hand an MCP client a file-writing tool. See mcp_contracts_dev.go.
		if dev.DevMCPEnabled() {
			if err := a.registerContractDevTools(); err != nil {
				a.Logger().Warn("dev MCP contract source tools partially skipped", "error", err)
			}
		}
	}
	// Mutating control tools, separately opted in (trusted /mcp only).
	if a.mcpControl {
		if err := a.registerControlTools(); err != nil {
			if !a.mcpControlDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP control tools partially skipped", "error", err)
		}
	}

	// MCP Apps (WithMCPApp): register each queued UI resource + linking
	// tool. An explicit host opt-in, so a collision is a hard error.
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

// Outbox returns the transactional event outbox, or nil when the app was
// built without WithOutbox. Use it to stage your own events inside an
// App.InTx transaction (Append), inspect delivery state (List), or replay
// dead rows.
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
		// Label it so a hook firing can be attributed to this entity in
		// the semantic-coverage manifest.
		reg.SetLabel(entityName)
		a.hooks[entityName] = reg
	}
	return a.hooks[entityName]
}

// lookupHookRegistry returns an entity's hook registry, or nil when it has
// none. Unlike HookRegistry it never creates one, so calling it on the
// request path cannot grow the map. Used to give ?include= children their
// own entity's read hooks.
//
// It still takes the read lock: kiln's build-mode runtime calls
// HookRegistry (the creating variant) from a live server, so registration is
// not strictly setup-only and an unguarded read would race a lazy create.
func (a *App) lookupHookRegistry(entityName string) *hook.HookRegistry {
	a.hooksMu.RLock()
	defer a.hooksMu.RUnlock()
	return a.hooks[entityName]
}

// OnStart registers a function to run once during boot (Run, Start or
// Serve), after plugin Init and before the host starts its transport (for
// Start, before the HTTP server begins accepting connections). The
// context passed in is the App's lifecycle context, cancelled when
// Shutdown begins, so workers should respect it.
//
// Hooks run in registration order; the first to return a non-nil error
// aborts the run. No later hook starts once Shutdown has begun. To stop
// the App from a hook, return an error: a Shutdown called synchronously
// from inside one waits out its own deadline, and with no deadline never
// returns (see Run).
func (a *App) OnStart(fn func(ctx context.Context) error) *App {
	a.startHooks = append(a.startHooks, fn)
	return a
}

// OnReady registers a function to run once the HTTP listener has bound
// successfully, just before the server begins accepting connections. The
// addr passed in is the listener's resolved address (a ":0" request
// arrives with the real port), so it is safe to print in a startup
// banner: every earlier phase, auto-migrate, seeds, plugin init, OnStart
// hooks, and the bind itself, has already succeeded. Hooks run in
// registration order and must not block, and none starts once Shutdown
// has begun. OnReady is HTTP transport readiness: Run with a host that
// binds no listener (ServicesHost) refuses an App that registers one.
// ServicesReady reports service readiness under any host.
func (a *App) OnReady(fn func(addr string)) *App {
	a.readyHooks = append(a.readyHooks, fn)
	return a
}

// OnStop registers a function to run during App.Shutdown, after the
// HTTP server has shut down. Hooks run in reverse registration order.
// The last thing started is the first thing stopped. Internally the
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
// RegisterPlugin(log) gets the order inverted on reverse iteration:
// log's close runs first, the user's OnStop logs into closed sinks.
func (a *App) OnStopFirst(fn func() error) *App {
	// Append so it runs LAST in the LIFO order used by PrependDrainer.
	a.lc.AppendDrainer(stopHookDrainer(fn))
	return a
}

// stopHookDrainer adapts a legacy OnStop func() error into the
// lifecycle.Drainer interface. The ctx is ignored. OnStop predates
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
//
// The stop side drains through StopContext so in-flight job goroutines
// are joined before shutdown proceeds, bounded by the drain deadline,
// so a job that ignores its (already-cancelled) context can't hang
// SIGTERM forever.
//
// Worker-scoped: under RoleServe this is a no-op. Neither the start hook
// nor the drainer is registered, so a serve-only shutdown never waits on
// a scheduler that was never started.
func (a *App) AddCron(s *cron.Scheduler) *App {
	if !a.runsWorkers() {
		return a
	}
	// If called during a module's Init, set the scheduler's gate so
	// jobs skip when the owning module is disabled.
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

// runStartHooks fires every OnStart hook with the app's lifecycle context.
// Battery lifecycle hooks are called first, then app-level start hooks.
// Returns the first error so Start aborts cleanly before binding the port.
// Once Shutdown has begun it starts nothing more and returns
// context.Canceled, which the engine treats as a graceful stop.
func (a *App) runStartHooks() error {
	a.ensureLifecycleContext()

	// Start batteries in dependency order
	if err := a.Batteries.StartAll(a.appCtx); err != nil {
		return err
	}
	if a.stopRequestedNow() {
		return context.Canceled
	}

	// Start the process-module supervisor's per-module loops (issue #37).
	// Each registered module's supervise goroutine launches here, after
	// InitPlugins has loaded module state. Shutdown drains the children
	// before batteries stop. StartLoops is idempotent.
	if a.processModules != nil {
		a.processModules.StartLoops()
	}

	// Then app-level start hooks (cron, queues, custom)
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

// runStartHookSafe fires one OnStart hook under the same
// recover-to-attributed-error isolation callModuleSafe gives Init:
// OnStart hooks are host/plugin callback code, and a panic in one must
// abort Start with an error instead of unwinding through it.
func (a *App) runStartHookSafe(fn func(ctx context.Context) error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(a.Logger(), "start hook", fmt.Sprintf("%T", v))
		}
	}()
	return fn(a.appCtx)
}

// Worker-scoped: under RoleServe this is a no-op. Neither the start hook
// nor the Close hook is registered, so a serve-only shutdown never closes
// a queue it never started.
// schedulerStartStop is the minimal interface AddQueue needs. We keep it
// here (not in the queue package) so framework doesn't have to import
// battery/queue, apps wire their queue manually and just hand the
// start/stop pair over.
type schedulerStartStop interface {
	Start(ctx context.Context)
	Close() error
}

func (a *App) AddQueue(q schedulerStartStop) *App {
	if !a.runsWorkers() {
		return a
	}
	// If called during a module's Init, duck-type SetGate on the queue
	// value (mirrors the SetFanout precedent in Mount). The gate defers
	// jobs whose owning module is disabled.
	if mod := a.modules.currentModule(); mod != "" {
		if g, ok := q.(interface{ SetGate(func(string) bool) }); ok {
			g.SetGate(a.modules.queueGate(mod))
		}
	}
	a.OnStart(func(ctx context.Context) error {
		q.Start(ctx)
		return nil
	})
	// The stop side bounds the Close join by the drain deadline, the
	// same posture AddCron gets from StopContext: a queue whose Close
	// blocks joining a ctx-ignoring job handler can't hang SIGTERM
	// forever. schedulerStartStop has no context-aware Close, so the
	// join is abandoned at the deadline (the underlying Close keeps
	// running) and the abandonment is reported as the drain error.
	a.lc.PrependDrainer(lifecycle.DrainFunc(func(ctx context.Context) error {
		return drainQueueClose(ctx, q)
	}))
	return a
}

// drainQueueClose runs q.Close and abandons the join when ctx expires.
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

// trackConnState is the server's ConnState hook: it records a
// connection while it sits in StateNew and forgets it the moment it
// does anything else (a request, an idle keep-alive, a hijack, a
// close). Shutdown closes what is still recorded.
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

// closeNewConns closes every connection still in StateNew. The set is
// copied under the lock and closed outside it: Close runs the
// ConnState hook (StateClosed), which takes the same lock.
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

// Shutdown gracefully stops the HTTP server, stops every registered
// battery in reverse dependency order, then runs each OnStop hook in
// reverse registration order. It first turns /readyz to 503 and, while
// the server is serving, keeps serving for the readiness grace
// (WithReadinessDrain, 5s by default) so a load balancer stops routing
// here before the listener closes; /healthz stays 200. The grace counts
// against ctx: it takes at most half of what ctx has left, and the rest
// of the teardown gets the remainder. Matches net/http.Server.Shutdown's
// signature (takes a deadline ctx) but does the FULL lifecycle teardown.
// Safe to call multiple times: the teardown runs once, and a later or
// concurrent call waits for it to finish, then returns nil, or returns
// ctx's error if its own ctx ends first.
//
// Called while the App is still booting, or while the OnStart hooks that
// interactive setup deferred are running, Shutdown cancels the lifecycle
// context, and the run tears down once its current phase returns, before
// any later phase or the host starts. A running HTTP host keeps serving
// until then, with /readyz answering 503. The teardown runs on ctx, with its
// deadline and values; Shutdown waits for it and returns its error, or
// returns ctx's error once ctx ends first. A teardown whose ctx ended
// before the phase returned runs on a fresh five-second budget that keeps
// ctx's values, so the cleanup is neither dropped nor unbounded. Called
// synchronously from inside a boot callback (a seed, an OnStart hook, a
// BootValidator), Shutdown waits for a teardown that cannot start until
// the callback returns: it returns at ctx's deadline, and with no
// deadline it never returns. Return an error from the callback instead,
// or call Shutdown on another goroutine. Called before any run, Shutdown
// tears down what was registered, and a later Run, Start or Serve returns
// nil without starting. The host's Stop runs where the HTTP server's
// drain always has: after the readiness grace and before batteries stop.
//
// Call this from your signal handler.
func (a *App) Shutdown(ctx context.Context) error {
	_, err := a.stop(ctx)
	return err
}

// stop is Shutdown. began reports whether this call began the teardown,
// so err is the teardown's own; a later or concurrent call only waits for
// it.
func (a *App) stop(ctx context.Context) (began bool, err error) {
	// A second call used to find the server already detached and go
	// straight to stopping batteries: sequentially every OnStop ran
	// twice, and concurrently (a signal handler plus a deferred Shutdown)
	// batteries closed under requests the first call was still draining
	// (2026-10-27 S3). Later callers wait on ctx as well as the teardown,
	// so a Shutdown called from inside the teardown is bounded by its own
	// deadline rather than deadlocked.
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
	// Record the stop under the lock admission, the end of boot and the
	// deferred start take, so no run starts its host, none starts at all,
	// and no deferred start begins after this.
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
	// /readyz answers 503 from the stop request on, not from the start of
	// the teardown's readiness drain: a stop handed to a running phase
	// postpones that drain until the phase returns, and while the
	// deferred start runs the HTTP host is already serving.
	a.draining.Store(true)
	if handoff {
		// A boot phase is running, or the deferred start interactive
		// setup postponed. Tearing down under it would stop batteries a
		// start hook is still starting and close the store a migration is
		// writing. Cancel it instead; the phase tears down on ctx when it
		// returns and passes the result back.
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

// shutdown is Shutdown's teardown, run once.
func (a *App) shutdown(ctx context.Context) error {
	a.drainReadiness(ctx)
	// Snapshot the cancel func and detach the server under the same lock
	// that Start/runStartHooks use to assign them. Invoke cancel OUTSIDE
	// the lock so a cancel-triggered teardown can't re-enter and deadlock.
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
	// A2A runs are detached from their requests, so nothing else stops
	// them. Cancel and wait for them before the HTTP drain, which a
	// blocking send would otherwise hold, and before the batteries close
	// the store a run's final state goes to.
	if a2aSrv != nil {
		if err := a2aSrv.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("stopping a2a runs: %w", err)
		}
	}
	// The host releases its transport next: the HTTP host drains its
	// server here (host_http.go), and a host stopped during boot releases
	// what it already holds.
	if host != nil {
		if err := a.stopHost(ctx, host); err != nil {
			firstErr = err
		}
	}

	// Process-module children drain next, while the batteries their
	// in-flight reverse calls reach are still running (design §8). The
	// drain closes the supervisor, so no loop restarts a drained child.
	if a.processModules != nil {
		_ = a.processModules.Drain(ctx)
	}

	// Stop batteries in reverse dependency order (dependents first)
	if err := a.Batteries.StopAll(ctx); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("stopping batteries: %w", err)
	}

	// Run OnStop hooks + battery-registered drainers through the
	// lifecycle coordinator. PrependDrainer in OnStop already encodes
	// the reverse-of-registration order callers expect.
	if err := a.lc.Shutdown(ctx); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("running stop hooks: %w", err)
	}
	return firstErr
}

// migrateAndSeed auto-migrates every registered entity, view and routine,
// unless the deployment opted into explicit migrations only
// (WithoutAutoMigrate), then runs entity seeds. Seeds run either way: they
// are idempotent data writes, not schema, and a seeded entity whose table
// is missing fails Start here rather than serving against an unmigrated
// schema. No DB, nothing to do.
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

// schemaSize counts the schema declarations Start migrates: entities and
// tables (every registered version), views and routines. All three only
// grow, so a change means something new to migrate.
func (a *App) schemaSize() int {
	return a.Registry.size() + len(a.migrationViews) + len(a.migrationRoutines)
}

// runSeedHooksSerialized runs the App.WithSeed hooks, serializing them across
// replicas behind the seed advisory lock (DISTINCT from the migration lock) so
// two booting replicas don't race a hook. A MaxOpenConns(1) Postgres pool
// cannot hold the lock without deadlocking the hooks' own queries, so it runs
// unlocked with a loud WARN (the same gap RunSeeds documents). SQLite / no-DB
// run unlocked (single-process; the lock pin would fight the pool). WithSeed
// hooks have no ledger. They serialize-per-boot but still run on every
// replica, so keep them idempotent.
func (a *App) runSeedHooksSerialized() error {
	a.ensureLifecycleContext()
	if a.DB == nil {
		return a.runSeedHooks()
	}
	// Fail closed on an unknown dialect: whether to take the advisory lock
	// is a coordination decision, and a transient probe failure on Postgres
	// must not demote hooks to the unlocked branch (the same invariant
	// AutoMigrate and RunSeeds hold). The probe goes through the App's
	// cache, so request paths that need the dialect (InTx, app_routines)
	// read the boot's answer instead of probing on a background context.
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

// ensureLifecycleContext lazily creates the app's cancellable lifecycle
// context under serverMu, the same lock that guards server. Run's
// admission creates it from the caller's context values; the seed and
// start hook phases call this for callers that run them directly. Shutdown
// reads/nils appCancel under the same lock, so a SIGTERM-driven Shutdown
// racing pre-listen setup is data-race free.
func (a *App) ensureLifecycleContext() {
	a.serverMu.Lock()
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}
	a.serverMu.Unlock()
}

// warnUnresolvableRelations names every declared relation whose target
// entity is not in the registry. Such a relation cannot be eager-loaded:
// ?include= refuses it at request time, because with no target entity
// there is no schema to drive the Hidden-column scrub, owner scope,
// tenant scope, soft-delete filter or scoped-filter allow-list, and a
// `SELECT *` without them is how a related auth table's password_hash
// reaches a caller.
//
// This is a boot diagnostic, not a gate: the refusal already happens at
// the request, and an app that never includes the relation is still
// correct. Warning moves the discovery from a 400 in production to the
// startup log.
func (a *App) warnUnresolvableRelations() {
	if a.Registry == nil {
		return
	}
	log := a.Logger()
	for _, ent := range a.Registry.AllSorted() {
		for _, rel := range ent.Config.Relations {
			// Resolve the way the request path does (framework/crud's
			// include.go and nested_filter.go both use ResolveTarget), or the
			// warning contradicts the runtime: Registry.Get reports an
			// ambiguity error for a name mounted under several versions, which
			// is exactly what App.GroupEntity produces, while ResolveTarget
			// picks the target matching the SOURCE entity's version and
			// resolves it fine.
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

// Start boots the app and serves it over HTTP on addr. It is Run with
// the built-in HTTP host: the lifecycle engine runs every boot phase
// (Run documents the order and the stop rules), then the host binds addr
// (worktree isolation may remap it), fires OnReady and serves until
// Shutdown, after which Start returns nil. Start installs a SIGINT and
// SIGTERM handler unless AppConfig.DisableSignalHandling is set.
func (a *App) Start(addr string) error {
	return a.run(context.Background(), &httpHost{app: a, addr: addr})
}

// Serve runs the app exactly as Start does (migrate, seed, init plugins,
// start hooks, the composed handler chain, OnReady, graceful Shutdown),
// but accepts connections on ln instead of binding a TCP address. It is
// the seam for a host that owns the transport: an in-process listener
// for a mobile shell's WebView, where no socket may be opened, or a
// listener a supervisor already holds. Worktree isolation does not
// remap a caller-supplied listener. OnReady hooks receive
// ln.Addr().String(). Serve closes ln when it returns, on every path,
// a refused or failed start included.
func (a *App) Serve(ln net.Listener) error {
	if ln == nil {
		return errors.New("framework: Serve: nil listener")
	}
	l := &onceCloseListener{Listener: ln}
	defer func() { _ = l.Close() }()
	return a.run(context.Background(), &httpHost{app: a, ln: l})
}

// onceCloseListener closes the caller's listener once however many
// paths close it (an early abort, start's own defer, http.Server's
// Shutdown): net.Listener does not promise an idempotent Close, and a
// hand-written one that closes a channel panics on the second call.
type onceCloseListener struct {
	net.Listener
	once sync.Once
	err  error
}

func (l *onceCloseListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	return l.err
}

// inProcessNetwork reports whether a listener's network is one nothing
// outside the process dials: "memory" or "pipe" (net.Pipe's), the
// in-memory listener a mobile shell feeds its WebView through. Every
// other network, an unknown one included (vsock, a proxy-fronted unix
// socket), may be reachable from outside and keeps the readiness drain.
func inProcessNetwork(network string) bool {
	return network == "memory" || network == "pipe"
}

// tcpNetwork reports whether a listener's network names a TCP address,
// the only kind whose String() is a host:port.
func tcpNetwork(network string) bool {
	return network == "tcp" || network == "tcp4" || network == "tcp6"
}

// resolveListenAddr applies worktree isolation to a Start address.
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
	// Worktree isolation deliberately remaps the listen address (see
	// isolation.md), but silently honoring a different port than the
	// one the caller assigned is configuration accepted-but-not-honored:
	// anything polling the requested port hangs to its deadline (#268).
	// Scream once with both addresses and both kill switches. Gated on
	// Active(): Addr also normalizes a bare "8080" to ":8080" with
	// isolation off, and that spelling change is not a remap.
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

// bootSetup checks first-run setup status (WithSetup). When incomplete
// and not disabled via GOFASTR_SETUP=off, the run either executes the
// steps inline (headless: every required env present) or reports
// interactive, and the HTTP host defers consumer startup and serves the
// wizard until the final step completes. Worker role + incomplete setup
// is a hard error: setup touches tables the worker's consumers would
// race on. So is a host with no HTTP listener (web false), which cannot
// serve the wizard.
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
		// Headless bootstrap finished. Proceed normally.
		return false, nil
	}
	if !web {
		return false, errors.New("setup incomplete: the interactive setup wizard needs an HTTP host; supply every required setup value so setup runs headless, or complete setup through Start once")
	}
	return true, nil
}

// finalizeRoutes registers the routes that need the complete app (the
// OpenAPI spec, llm.md, metrics, MCP and A2A mounts, well-known
// documents, the API 404, health), then runs the deferred BootValidator
// checks. It reports which documents the startup banner may advertise.
func (a *App) finalizeRoutes() (hasAPI, hasLLMMD bool, err error) {
	// Auto-generate and serve OpenAPI spec. Only when the app actually
	// declares entities, a UI-only app (e.g. a content site) has an
	// empty registry and gets none of these routes. The startup banner
	// keys off the same flags so it never advertises a 404.
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

		// API entity index under /api/ alongside /api/docs/.
		// Root /llm.md is free for the homepage screen doc.
		if !a.Config.NoLLMMD {
			pagesIndex := a.pageIndexPath()
			// PublicOpenAPI serves the index without the auth gate, the
			// same opt-in and exposure class as the public /openapi.json
			// above (llm.md derives from the same schema information, and
			// the startup banner already advertises both URLs behind this
			// single switch). The public route serves the full
			// construction-time document: the operator just opted into
			// publishing the complete spec, which discloses strictly more
			// than this index. The default route stays the session-gated,
			// per-request-filtered crud handler, and per-entity
			// /{table}/llm.md routes keep their own scope gate either way.
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

	// Prometheus /metrics endpoint when metrics are enabled (WithMetrics).
	// Unauthenticated by design, scrape from inside the network boundary.
	if a.metrics != nil {
		// Through Metrics(), not the bare store: it attaches the
		// framework-owned collectors (db_pool, outbox), which otherwise
		// appeared only once a battery happened to take the handle.
		a.router.Get("/metrics", middleware.MetricsHandler(a.Metrics()))
	}

	if a.Config.DebugEndpoints {
		a.registerDebugEndpoints()
	}
	// MCP endpoint (agent-readiness). WithMCP() exposes the server's
	// tools at /mcp via Streamable HTTP (POST JSON-RPC + GET SSE) so a
	// host doesn't hand-wire the route.
	// Advertise the app's name in the MCP initialize handshake when set,
	// so a connecting client sees a meaningful serverInfo.name.
	if a.Config.Name != "" {
		a.MCP.SetServerName(a.Config.Name)
	}
	if a.mcpAutoMount {
		// A dev-implied mount yields to a hand-wired /mcp route (older
		// scaffolds mount POST /mcp themselves), dev must not turn a
		// previously working app into a route-conflict panic. Explicit
		// WithMCP() keeps the documented panic: pick one.
		if a.mcpMountDevImplied && a.routerHasMCPRoute() {
			a.Logger().Warn("dev MCP auto-mount skipped: /mcp is already mounted by the host")
		} else {
			h := a.MCP.ServeSSE("/mcp")
			a.router.Post("/mcp", h)
			a.router.Get("/mcp", h)
		}
	}
	// WithMCPApp widgets need /mcp reachable. Warn (don't fail) when an app is
	// registered but nothing will serve /mcp, a silent prod-only 404 that
	// works under the dev auto-mount and disappears in production.
	if len(a.mcpApps) > 0 && !a.mcpAutoMount && !a.routerHasMCPRoute() {
		a.Logger().Warn("WithMCPApp registered but /mcp is not mounted: the widget and its tool will be unreachable; add framework.WithMCP()")
	}
	// Entity MCP tools (Exposure.MCP: true) have the same prod-only 404
	// failure mode: dev auto-mounts /mcp so the tools work locally, a
	// production binary without WithMCP() registers them onto a dispatcher
	// nothing can reach. Warn when at least one entity opted in and its
	// tools were actually registered (DB present).
	if a.DB != nil && !a.mcpAutoMount && !a.routerHasMCPRoute() {
		for _, e := range a.Registry.All() {
			if e.Config.Exposure != nil && e.Config.Exposure.MCP {
				a.Logger().Warn("entity MCP tools are registered but /mcp is not mounted: they will be unreachable; add framework.WithMCP()")
				break
			}
		}
	}
	// Agent2Agent task exchange (WithA2A). Mounted here, after every
	// MCP tool is registered, because the derived entity skills are
	// built from a.MCP.ListTools().
	if a.a2aCfg != nil {
		if err := a.mountA2A(); err != nil {
			return hasAPI, hasLLMMD, err
		}
	}
	if a.mcpAutoMount {
		a.router.Get("/mcp/server-card", http.HandlerFunc(a.handleMCPServerCard))
		a.router.Get("/.well-known/mcp/catalog.json", http.HandlerFunc(a.handleMCPCatalog))
	}
	// OAuth Protected Resource metadata (RFC 9728). Opt-in; advertises how
	// OAuth-token-protected resources accept tokens.
	if a.oauthResource != nil {
		for _, p := range a.oauthResource.metadataPaths() {
			a.router.Get(p, http.HandlerFunc(a.handleOAuthProtectedResource))
		}
	}
	// Agent-readiness well-known endpoints (isitagentready.com). The API
	// catalog (RFC 9727 linkset) + MCP server card are auto-served when
	// their precondition holds (has an API / MCP exposed); the agent
	// skills index is always served (empty list passes discovery);
	// OAuth authorization server (RFC 8414) is opt-in.
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

	// Machine-readable misses in the API namespace (agent-readiness): a
	// path under the configured APIPrefix (default /api) that no route
	// owns, or that a route owns under another method, must answer
	// application/problem+json, not the UI host's HTML 404 or 405.
	// Installed after every mount above so the wrapped fall-throughs are
	// the final ones. See api_miss.go.
	a.installAPIMissProblems()

	// Auto-register a db readiness probe if a DB is configured. Plugins
	// and batteries that implement ReadinessRegistrar were given a chance
	// to add their own during InitPlugins above.
	if a.DB != nil {
		c := dbReadinessCheck(a)
		a.RegisterReadiness(c.Name, c.Check)
	}
	a.registerHealthEndpoints()

	// Deferred mountable validation (framework.BootValidator): checks
	// that need the COMPLETE route table — the UI host's strict
	// internal-link check resolves chrome hrefs against everything the
	// app will serve, and batteries, plugins, and Start itself all
	// register routes after Mount. This is the last stop before the
	// listener binds: the latest point at which a finding can still
	// refuse to serve. Same panic contract as a Mount-time check.
	a.checkLocaleRoutes()
	for _, m := range a.mountables {
		if bv, ok := m.(BootValidator); ok {
			bv.ValidateBoot()
		}
	}

	return hasAPI, hasLLMMD, nil
}

// runReadyHookSafe fires one OnReady hook under the recover-to-error
// isolation the Init and OnStart phases already get: OnReady hooks are
// host callback code that runs after the listener is bound, and a panic
// in one must surface as Start's abort error (draining the bound
// listener through the normal teardown) rather than crashing the boot.
func (a *App) runReadyHookSafe(fn func(addr string), addr string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(a.Logger(), "ready hook", fmt.Sprintf("%T", v))
		}
	}()
	fn(addr)
	return nil
}

// printStartupBanner reports readiness only after the listener has bound.
// boundAddr is the resolved listener address, so Start("127.0.0.1:0") prints
// the actual port and a bind failure prints nothing.
//
// A listener that is not TCP (Serve on an in-process listener, the
// mobile shell's shape, or on a unix socket) has no URL anyone can open,
// so the banner names the listener and prints bare paths instead of
// advertising http://memory/posts, or an http:// URL built from a socket
// path shaped like 127.0.0.1:8080.
func (a *App) printStartupBanner(boundAddr, network, name string, hasAPI, hasLLMMD bool, setupURL string) {
	base := a.urlScheme() + "://" + boundAddr
	listening := base
	if _, _, err := net.SplitHostPort(boundAddr); err != nil || !tcpNetwork(network) {
		base, listening = "", boundAddr+" (not a TCP address)"
		// The setup URL names the listener as its host unless a host
		// name is configured; keep only the path then, like every
		// other line.
		setupURL = strings.TrimPrefix(setupURL, a.urlScheme()+"://"+boundAddr)
	}
	w := a.startupOutput
	if w == nil {
		w = os.Stdout
	}
	if a.role == RoleWorker {
		// The worker serves only the health surface, advertising entity or
		// API routes here would advertise 404s.
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
		// Same predicate as route registration: an entity without CRUD
		// routes (Exposure.CRUD=false, or no DB) has no URL to
		// advertise — printing one hands the reader a 404 (#244).
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
		// An anonymous curl of a non-Public entity answers 401; advertising
		// the URL without saying so reads as "the framework is broken" to a
		// first-run user.
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
	// "(requires auth)" on every line says what an anonymous curl gets, not
	// how to get past it. With nothing noted that signs callers in
	// (NoteAuthenticator), the gated URLs are unreachable for everyone, so
	// the banner names the two ways out once (#503).
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

	// The MCP endpoint is the one surface a first-run user cannot discover by
	// guessing a URL, and it was the only mounted surface the banner never
	// named, an app could serve a full tool set at /mcp and say nothing about
	// it.
	//
	// The note covers ENTITY tools, which re-enter the app router and so answer
	// an anonymous call with the same error their REST route would. The
	// introspection tools (app_routes, app_config, …) are a separate, ungated
	// surface. See `gofastr docs app-spec`, so the wording says "your app's
	// permissions" rather than claiming every tool is gated.
	// a.MCP is optional and nil-checked elsewhere in this file, and this line
	// runs AFTER the listener has bound, a panic here kills a server that is
	// already accepting connections, to print a banner.
	if a.mcpAutoMount && a.MCP != nil {
		mcpNote := ""
		if n := len(a.MCP.ListTools()); n > 0 {
			mcpNote = fmt.Sprintf("  (%d tools, your app's permissions)", n)
		}
		fmt.Fprintf(w, "  %s MCP:         %s/mcp%s\n", arrow(), base, mcpNote)
	}
	_, _ = fmt.Fprintln(w)
}

// NoteAuthenticator records that something on this app signs callers
// in: battery/auth's AuthManager calls it from Init, whether the app
// registers the battery or calls Init itself. A custom auth layer calls
// it where it installs its middleware. Until one is noted, the startup
// banner adds a line saying the auth-gated URLs it lists answer 401 to
// everyone.
func (a *App) NoteAuthenticator() { a.authenticatorNoted.Store(true) }

// AuthenticatorNoted reports whether NoteAuthenticator has been called.
func (a *App) AuthenticatorNoted() bool { return a.authenticatorNoted.Load() }

// debugAllowed gates /.debug/*: 401 without a user, 403 for an embed grant,
// a Decider deny, a scope-restricted caller without access.RoleScope("admin"),
// or a caller WithDebugAuthorize (default: the "admin" role) refuses. A
// signed-in user is not enough. These endpoints carry the pid, the memory
// layout and every leaked goroutine's stack, and on most apps anyone can
// sign up.
func (a *App) debugAllowed(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if u, ok := handler.GetUser(ctx); !ok || u == nil {
		i18n.HTTPError(w, r, http.StatusUnauthorized, i18n.StatusPhrase(http.StatusUnauthorized))
		return false
	}
	allowed := false
	if _, embedded := fembed.GrantFromContext(ctx); !embedded && !deciderDenies(ctx) {
		if a.debugAuthorize != nil {
			// A token carries its owner's identity, so a predicate that
			// admits the owner would admit a token minted for any other
			// surface. A scoped caller needs "role:admin" (or "role:*",
			// "*:*") first, as the default gate's callerRoles demands.
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

// hasDebugAdminRole reports whether the caller holds "admin", through the
// user's GetRoles() (battery/auth's User) or the roles access middleware
// resolved onto the context.
func hasDebugAdminRole(ctx context.Context) bool {
	return slices.Contains(callerRoles(ctx), "admin")
}

// registerDebugEndpoints adds /.debug/stats and /.debug/goroutineleak for
// runtime diagnostics. Both expose process internals, so both sit behind
// debugAllowed: an authenticated admin, or whoever WithDebugAuthorize
// admits.
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
		// Leak detection is GC-reachability-based; force a cycle so the
		// report reflects now, not the last collection.
		runtime.GC()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// debug=1 is the leak report; debug>=2 degrades to ALL goroutine
		// stacks (leaked or not) by stdlib design.
		_ = p.WriteTo(w, 1)
	}))
}

// goroutineLeakCount reports how many goroutines the runtime has proven
// leaked (blocked forever on unreachable primitives). Forces a GC cycle
// first — detection happens during collection.
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
