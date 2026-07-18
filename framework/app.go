package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/featureflag"
	"github.com/DonaldMurillo/gofastr/core/handler"
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
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/lifecycle"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/outbox"
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

	// PublicOpenAPI serves /openapi.json without the auth gate. By default
	// the spec is auth-gated (it enumerates every route), so a minimal app
	// returns 401 there — which surprised users following the quickstart
	// curl. Set true (or use WithPublicOpenAPI) when the spec is meant to be
	// public, e.g. a docs site or an internal API behind a network boundary.
	// The Swagger UI at /api/docs/ is always reachable; this only governs
	// the raw spec JSON.
	PublicOpenAPI bool

	// APIPrefix mounts every auto-CRUD entity route (list/get/create/update/
	// delete + _batch + _events + per-entity llm.md) under this path — e.g.
	// "/api" serves GET /api/posts instead of GET /posts. Empty (default)
	// keeps the bare entity-name mounts, so this is not a breaking change.
	// The generated OpenAPI spec expresses the prefix via its server URL.
	// GroupEntity routes are unaffected — a group owns its own prefix. MCP
	// tool names are unchanged.
	APIPrefix string

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

	// SecurityHeaders configures the defensive HTTP response headers
	// (Content-Security-Policy, Referrer-Policy, X-Frame-Options,
	// Permissions-Policy, CORP, COOP, HSTS) emitted by the SecurityHeaders
	// middleware in the default chain. The zero value installs the
	// framework's strict defaults (default-src 'self', no 'unsafe-inline');
	// set fields to override — e.g. ContentSecurityPolicy to allow
	// style-src 'unsafe-inline' for a third-party CSS dependency. Unset
	// fields keep their built-in defaults, so a partial override never
	// silently drops a defensive header. See [WithSecurityHeaders].
	SecurityHeaders middleware.SecurityHeadersConfig

	// ShutdownTimeout bounds the graceful drain that the default
	// SIGINT/SIGTERM handler runs via App.Shutdown. Zero installs the
	// 15s default. The drain stops accepting connections, waits for
	// in-flight requests, force-closes whatever remains at the deadline
	// (an open SSE stream never goes idle), then stops batteries and
	// runs OnStop hooks.
	ShutdownTimeout time.Duration

	// DisableSignalHandling opts out of the SIGINT/SIGTERM handler that
	// Start installs by default. Set it when the embedding process owns
	// signal handling and calls App.Shutdown (or RunWithSignals) itself.
	DisableSignalHandling bool
}

// defaultShutdownTimeout bounds the signal-triggered drain when
// AppConfig.ShutdownTimeout is unset. Kubernetes gives pods 30s between
// SIGTERM and SIGKILL by default; finishing well inside that budget
// leaves room for the pre-stop hook and container runtime overhead.
const defaultShutdownTimeout = 15 * time.Second

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

	migrationRoutines []migrate.Routine // stored procedures/functions/triggers run on boot
	migrationViews    []migrate.View    // views (virtual tables built from entities) run on boot

	Batteries *BatteryManager

	// modules manages registered modules and their enable/disable state.
	// Created in NewApp; nil-safe (NewModuleManager always returns non-nil).
	modules *ModuleManager

	// processModules supervises out-of-process third-party modules
	// (issue #37, wave 2a). Lazily constructed by RegisterProcessModule;
	// nil when the app has no process modules. StartLoops + PrependDrainer
	// are wired in [App.Start].
	processModules *ProcessModuleSupervisor

	// moduleTools is the MCP tool registry shared across every process
	// module (design §5.1). Lazily constructed alongside processModules;
	// nil when the app has no process modules. The supervisor uses it as
	// its ToolRegistrar; the composite MCP call gate consults it so a
	// disabled module's tools are omitted from tools/list.
	moduleTools *ModuleToolRegistry

	// processDrainRegistered guards the supervisor's PrependDrainer call
	// so repeated Start paths (canonical + setup-interactive) don't
	// double-register. Read/written under no lock — runStartHooks is
	// single-threaded through Start.
	processDrainRegistered bool

	serverMu   sync.Mutex // guards server + appCtx/appCancel — Start writes, Shutdown reads/nils
	server     *http.Server
	events     *event.EventBus
	hooks      map[string]*hook.HookRegistry
	mountables []Mountable
	noDefaults bool

	// outbox is the transactional event outbox (WithOutbox). When set,
	// CRUD lifecycle events are staged in the write transaction and a
	// relay — started by Start, drained on Shutdown — delivers each to
	// the declared durable consumers (WithOutboxConsumer). The relay no
	// longer touches the live bus; the real-time lane (EmitEvent) feeds
	// SSE and ephemeral subscribers independently.
	outbox          *outbox.Outbox
	outboxOpts      []outbox.Option
	outboxEnabled   bool
	outboxConsumers []outboxConsumerDecl

	// fanout, when set (WithFanout), bridges the real-time lane across
	// replicas: the event bus is attached in NewApp and any Mountable that
	// supports SetFanout (a mounted UI host's island manager) is wired at
	// Mount time. Caller-owned — the app never closes it.
	fanout fanout.Fanout

	// noAutoMigrate suppresses the boot-time entity auto-migration
	// (WithoutAutoMigrate) for deployments that require every schema
	// change to be an explicit, operator-invoked step.
	noAutoMigrate bool

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

	// readyHooks fire once the HTTP listener has bound, just before the
	// server starts accepting connections — see App.OnReady.
	readyHooks []func(addr string)

	// seedHooks run during Start AFTER auto-migration (tables exist) and
	// before plugin/battery init — see App.WithSeed.
	seedHooks []func(ctx context.Context) error

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

	// Optional locale resolver (set via WithLocaleResolver). Consulted
	// before X-Locale / Accept-Language during negotiation so a stored
	// per-user locale (cookie) can win. Only effective with WithI18n.
	localeResolver func(*http.Request) (string, bool)

	// Optional metrics. When set (via WithMetrics), the metrics middleware
	// joins the default chain and a Prometheus /metrics endpoint is mounted.
	metrics *middleware.Metrics

	// tracing enables the OpenTelemetry tracing middleware in the default
	// chain (via WithTracing). Spans no-op until a TracerProvider is wired
	// through otel.SetTracerProvider.
	tracing bool

	// mcpIntrospection enables a set of read-only MCP tools that expose
	// the app's routes, plugins, batteries, config, and readiness state
	// for agent debugging. Set via WithMCPIntrospection().
	mcpIntrospection bool
	// mcpControl enables the MUTATING MCP tools (module enable/disable)
	// for trusted /mcp endpoints. Set via WithMCPControl().
	mcpControl bool
	// mcp*DevImplied mark surfaces the dev loop turned on (GOFASTR_DEV,
	// see NewApp) rather than an explicit option. Dev-implied surfaces
	// tolerate collisions (hand-mounted /mcp, same-named tools) with a
	// warning; explicit ones keep their documented panic/error.
	mcpMountDevImplied         bool
	mcpIntrospectionDevImplied bool
	mcpControlDevImplied       bool
	// mcpAutoMount exposes the MCP server at /mcp (Streamable HTTP:
	// POST JSON-RPC + GET SSE) without the host hand-wiring the route.
	// Set via WithMCP(). Makes the server's tools reachable at the
	// canonical endpoint for agent-readiness.
	mcpAutoMount bool
	// oauthResource, when set, serves /.well-known/oauth-protected-resource
	// (RFC 9728) so OAuth-token-protected APIs are discoverable. Set via
	// WithOAuthProtectedResource().
	oauthResource *OAuthProtectedResourceConfig
	// agentSkills, when set, serves /.well-known/agent-skills/index.json
	// (agent-skills-discovery-rfc). Set via WithAgentSkills().
	agentSkills []AgentSkillEntry
	// oauthAuthServer, when set, serves /.well-known/oauth-authorization-server
	// (RFC 8414). Set via WithOAuthAuthorizationServer().
	oauthAuthServer *OAuthAuthorizationServerConfig
	// authMD, when set, serves /auth.md + merges an agent_auth block into
	// the OAuth authorization-server metadata (WorkOS agentic-registration).
	authMD *AuthMDConfig
	// webBotAuth/ucp/acp: opt-in commerce + bot-signing discovery docs
	// (isitagentready.com production checks). Set via WithWebBotAuth /
	// WithUCP / WithACP.
	webBotAuth *WebBotAuthConfig
	ucp        *UCPConfig
	acp        *ACPConfig

	// startupOutput receives the human-readable readiness banner. It defaults
	// to os.Stdout and stays unexported so tests can verify startup ordering
	// without replacing the process-wide stdout file descriptor.
	startupOutput io.Writer

	// role is the resolved process role (all/serve/worker), set once in
	// NewApp from WithRole > GOFASTR_ROLE > RoleAll. AddCron/AddQueue and
	// the outbox relay gate on it so RoleServe doesn't start work it can't
	// drain. roleOpt/roleSet carry the raw WithRole value until resolution.
	role    Role
	roleOpt Role
	roleSet bool

	// setup is the optional first-run setup runner (WithSetup). When set
	// and setup is incomplete at boot, Start either runs the steps
	// headlessly (all required env present) or serves an interactive
	// wizard until setup finishes — then atomically swaps to the real
	// router. See SetupRunner for the full lifecycle.
	setup SetupRunner

	// handlerCell holds the currently-serving http.Handler behind an
	// atomic.Pointer so the interactive-setup swap can switch from the
	// wizard surface to the real app router without a restart. We use a
	// wrapper struct (not atomic.Value) because atomic.Value panics when
	// the concrete type changes between Stores (http.HandlerFunc vs
	// *router.Router). The wrapper normalizes both to *servingHandler.
	handlerCell atomic.Pointer[servingHandler]
}

// servingHandler wraps an http.Handler so it can be stored in an
// atomic.Pointer without panicking on concrete-type changes during the
// setup-to-real-router swap.
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

// WithConfig sets the application config. It merges into whatever the
// granular options (WithAPIPrefix, WithPublicOpenAPI, WithName, …) have
// already set rather than replacing the struct wholesale, so option order
// doesn't silently discard config: every field WithConfig sets to a
// non-zero value wins; a zero field preserves the existing value. To turn
// a boolean back off, use the granular setter after WithConfig instead of
// relying on a zero-valued field.
//
// TestWithConfigCoversEveryField pins the field list — extend this merge
// when adding an AppConfig field.
func WithConfig(config AppConfig) AppOption {
	return func(a *App) {
		if config.Name != "" {
			a.Config.Name = config.Name
		}
		if config.JSONCase != "" {
			a.Config.JSONCase = config.JSONCase
		}
		if config.DebugEndpoints {
			a.Config.DebugEndpoints = true
		}
		if config.NoLLMMD {
			a.Config.NoLLMMD = true
		}
		if config.PublicOpenAPI {
			a.Config.PublicOpenAPI = true
		}
		if config.APIPrefix != "" {
			a.Config.APIPrefix = config.APIPrefix
		}
		if config.RequestTimeout != 0 {
			a.Config.RequestTimeout = config.RequestTimeout
		}
		if config.DisableRequestTimeout {
			a.Config.DisableRequestTimeout = true
		}
		// SecurityHeaders is a value type; copy it unconditionally. The
		// zero value is valid (means "use the built-in strict defaults"),
		// so there is no sentinel to gate on — unlike the scalar fields.
		a.Config.SecurityHeaders = config.SecurityHeaders
		if config.ShutdownTimeout != 0 {
			a.Config.ShutdownTimeout = config.ShutdownTimeout
		}
		if config.DisableSignalHandling {
			a.Config.DisableSignalHandling = true
		}
	}
}

// WithAPIPrefix mounts auto-CRUD entity routes under prefix (e.g. "/api").
// See AppConfig.APIPrefix. A leading slash is added and a trailing slash
// trimmed, so "api", "/api", and "/api/" all behave identically.
func WithAPIPrefix(prefix string) AppOption {
	return func(a *App) {
		a.Config.APIPrefix = prefix
	}
}

// WithPublicOpenAPI serves /openapi.json without the auth gate. Equivalent to
// setting AppConfig.PublicOpenAPI. Use when the spec is meant to be public
// (docs sites, internal APIs behind a network boundary).
func WithPublicOpenAPI() AppOption {
	return func(a *App) {
		a.Config.PublicOpenAPI = true
	}
}

// WithSecurityHeaders configures the defensive HTTP response headers
// (Content-Security-Policy, Referrer-Policy, X-Frame-Options, …) emitted
// by the SecurityHeaders middleware in the default chain. Equivalent to
// setting AppConfig.SecurityHeaders. The zero value keeps the framework's
// strict defaults; override individual fields (e.g.
// ContentSecurityPolicy) to relax a specific directive — unset fields
// retain their built-in defaults so a partial override never drops a
// defensive header. See middleware.SecurityHeadersConfig.
//
// This removes the need to shadow the default middleware with a
// hand-rolled SecurityHeaders middleware just to change one directive.
func WithSecurityHeaders(cfg middleware.SecurityHeadersConfig) AppOption {
	return func(a *App) {
		a.Config.SecurityHeaders = cfg
	}
}

// apiPrefix returns the normalized API prefix: "" when unset, otherwise a
// single leading slash and no trailing slash (e.g. "/api"). Tolerates the
// "api", "/api/", "//api" forms a user might set via WithConfig.
func (a *App) apiPrefix() string {
	p := strings.Trim(a.Config.APIPrefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// entityMountPath is the base path an entity's CRUD routes mount at —
// apiPrefix + "/" + table. With no prefix this is the historical "/table".
func (a *App) entityMountPath(table string) string {
	return a.apiPrefix() + "/" + table
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

// WithMCP exposes the app's MCP server at /mcp using the Streamable HTTP
// transport (POST JSON-RPC + GET Server-Sent Events), so a host doesn't
// have to hand-wire fwApp.Router().Handle("POST", "/mcp", fwApp.MCP). This
// is the agent-ready default: combined with uihost.WithAgentReady (which
// advertises /mcp via the agent card + Link headers) it makes the server's
// tools discoverable to MCP clients. Calling this AND manually mounting
// /mcp will panic with a route conflict — pick one.
func WithMCP() AppOption {
	return func(a *App) {
		a.mcpAutoMount = true
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

// WithoutAutoMigrate disables the entity auto-migration that App.Start
// otherwise runs before serving. Use it in deployments whose policy
// forbids unattended schema changes on boot: generate the entity DDL
// into versioned migration files instead (`gofastr migrate generate
// <name>`) and apply them as an explicit step (`gofastr migrate up`). Entity seeds still run at Start —
// they are idempotent data, not schema — which also means an entity
// WITH seeds fails Start fast when its table is missing, instead of the
// app serving against an unmigrated schema.
func WithoutAutoMigrate() AppOption {
	return func(a *App) {
		a.noAutoMigrate = true
	}
}

// WithOutbox enables the transactional event outbox (framework/outbox):
// CRUD lifecycle events are written to an outbox table INSIDE the same
// transaction as the entity write, and a relay goroutine (started by
// App.Start, drained on Shutdown) delivers each committed row to the
// declared durable consumers. This closes the crash window where a plain
// post-commit emit is lost, and makes delivery at-least-once per consumer
// — consumers that care must dedupe on Event.ID. Requires WithDB; NewApp
// panics otherwise.
//
// The relay delivers ONLY to consumers declared via [WithOutboxConsumer];
// it no longer publishes to the live event bus. The real-time lane (SSE
// EventStream, ephemeral On/Subscribe) is fed independently by EmitEvent,
// so the two lanes never duplicate. opts are forwarded to outbox.New
// (outbox.WithTable, outbox.WithPollInterval, …).
func WithOutbox(opts ...outbox.Option) AppOption {
	return func(a *App) {
		a.outboxEnabled = true
		a.outboxOpts = opts
	}
}

// outboxConsumerDecl captures a WithOutboxConsumer declaration until the
// outbox is constructed in NewApp, then registered on it.
type outboxConsumerDecl struct {
	name      string
	eventType string
	handler   event.EventHandler
}

// WithOutboxConsumer declares a durable outbox consumer. Requires
// WithOutbox (NewApp panics if the outbox isn't also enabled). name is a
// stable identity used to track per-consumer delivery across
// restarts/replicas; (eventType, name) must be unique. handler is invoked
// once per delivery with Event.ID set to the outbox row id (dedup key) —
// it must be idempotent (at-least-once delivery). A handler that errors
// or panics is retried with backoff and eventually dead-lettered
// independently of its sibling consumers (sibling isolation).
func WithOutboxConsumer(name, eventType string, handler event.EventHandler) AppOption {
	return func(a *App) {
		a.outboxConsumers = append(a.outboxConsumers, outboxConsumerDecl{name, eventType, handler})
	}
}

// WithFanout attaches a cross-replica fanout (core/fanout.Fanout) to the
// app's real-time lane. The event bus is bridged — every locally-emitted
// event is mirrored to the other replicas and re-emitted on their buses, so
// entity `_events` SSE streams work regardless of which replica holds the
// connection — and any Mountable that supports SetFanout (a mounted UI
// host) gets its island manager wired the same way.
//
// SEMANTICS: with a fanout attached the bus becomes a broadcast — every
// On/Subscribe handler fires on EVERY replica. That is correct for UI push
// and wrong for side effects; per-event work belongs on the durable lane
// (WithOutboxConsumer). Handlers that derive new events must gate on
// event.IsRemote(ctx). See the events doc, "Cross-replica fan-out".
//
// The fanout is caller-owned: construct it before NewApp (e.g.
// framework/fanout.NewPostgres) and close it after Shutdown. The bridge
// itself is detached by Shutdown. Panics if f is nil.
func WithFanout(f fanout.Fanout) AppOption {
	if f == nil {
		panic("framework: WithFanout(nil) — construct a fanout first (framework/fanout.NewPostgres, core/fanout.NewRedis)")
	}
	return func(a *App) {
		a.fanout = f
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

// WithMetrics enables HTTP request metrics (per-route counts, status classes,
// latency histograms) in the default middleware chain and mounts a
// Prometheus-format /metrics endpoint. The endpoint is unauthenticated by
// design (scrape it from inside your network / behind your ingress). Panics
// when paired with WithoutDefaultMiddleware — mount middleware.MetricsMiddleware
// and middleware.MetricsHandler yourself in that case.
func WithMetrics() AppOption {
	return func(a *App) {
		a.metrics = middleware.NewMetrics()
	}
}

// WithTracing enables the OpenTelemetry tracing middleware in the default
// chain. Each request runs in a span with method/route/status attributes.
// Spans no-op until you install a TracerProvider via otel.SetTracerProvider
// (e.g. an OTLP exporter), so this is safe to leave on. Panics when paired
// with WithoutDefaultMiddleware.
func WithTracing() AppOption {
	return func(a *App) {
		a.tracing = true
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

// WithLocaleResolver installs a custom locale resolver consulted BEFORE
// the X-Locale / Accept-Language headers during locale negotiation. Use
// it to make a stored per-user locale (e.g. a cookie set by a "change
// language" handler) win over the browser's Accept-Language.
//
// Pair with [i18n.CookieLocale] for the common cookie case:
//
//	a := framework.NewApp(
//	    framework.WithI18n(tr),
//	    framework.WithLocaleResolver(i18n.CookieLocale("locale")),
//	)
//
// Panics if used without WithI18n — locale resolution is meaningless
// without a translator/catalog to resolve against.
func WithLocaleResolver(f func(*http.Request) (string, bool)) AppOption {
	return func(a *App) {
		if a.translator == nil {
			panic("framework: WithLocaleResolver requires WithI18n — install a translator first")
		}
		a.localeResolver = f
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
	var localeResolver func(*http.Request) (string, bool)
	if a != nil {
		getLogger = a.Logger
		if a.Config.RequestTimeout > 0 {
			timeout = a.Config.RequestTimeout
		}
		idempotency = a.idempotency
		translator = a.translator
		localeResolver = a.localeResolver
	}
	chain := []router.Middleware{
		middleware.RecoveryFn(getLogger),
		middleware.RequestID(),
	}
	// Stamp the App's *sql.DB onto each request context so screens and
	// handlers can reach it via DBFromContext without a package-level
	// global. No-op when the App has no DB.
	if a != nil && a.DB != nil {
		chain = append(chain, a.DBContextMiddleware())
	}
	if a != nil && a.tracing {
		chain = append(chain, middleware.Tracing())
	}
	if a != nil && a.metrics != nil {
		chain = append(chain, middleware.MetricsMiddleware(a.metrics))
	}
	if idempotency != nil {
		chain = append(chain, middleware.Idempotency(*idempotency))
	}
	if translator != nil {
		var negOpts []i18n.NegotiateOption
		if localeResolver != nil {
			negOpts = append(negOpts, i18n.WithLocaleResolver(localeResolver))
		}
		chain = append(chain, i18n.Middleware(translator, negOpts...))
		// Bridge: stash the translator on the request ctx so framework/ui
		// components (DataTable, FilterToolbar, Carousel, …) resolve their
		// labels via i18nui.T(r.Context(), …) using the caller's locale.
		// Without this the components silently render English even when a
		// catalog is wired, because i18n.Middleware only attaches the
		// Locale — not the translator. Framework may import i18nui; core
		// may not, which is why this bridge lives here and not in core/i18n.
		chain = append(chain, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := i18nui.WithTranslator(r.Context(), translator)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		})
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
	// Wire the mountable into the cross-replica fanout (WithFanout) when it
	// supports it — a mounted UI host forwards to its island manager so
	// island push reaches sessions connected to other replicas. Duck-typed:
	// the framework doesn't import uihost.
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
	return a
}

// Mountables returns the Mountables registered via Mount, in registration
// order. Batteries use it to discover a mounted UI host (type-asserting to
// *uihost.Host) so they can register screens on the host's render pipeline
// rather than spinning up a second host. Returns a copy — callers must not
// mutate the App's internal slice.
func (a *App) Mountables() []Mountable {
	out := make([]Mountable, len(a.mountables))
	copy(out, a.mountables)
	return out
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
		// Pre-flight collision check against the full group-prefixed path,
		// mirroring App.Entity. Routes() records full (prefix-applied)
		// patterns, so compare against g.Prefix()+"/"+table.
		if msg := a.entityRouteCollision(name, g.Prefix()+"/"+e.GetTable()); msg != "" {
			panic("framework: " + msg)
		}
		crudHandler = crud.NewCrudHandler(e, a.DB)
		crudHandler.JSONCase = a.JSONCasing()
		crudHandler.Hooks = a.HookRegistry(name)
		crudHandler.Storage = a.Storage
		crudHandler.Events = a.Events()
		// Guarded assignment: a bare `= a.outbox` would wrap a typed nil
		// in the EventOutbox interface and silently swallow every event.
		if a.outbox != nil {
			crudHandler.Outbox = a.outbox
		}
		crudHandler.Registry = a.Registry

		// Register CRUD routes on the group's sub-router.
		// The group's prefix is already baked into the sub-router,
		// so we just mount at /<entity-table>.
		crud.RegisterCrudRoutes(g.Router(), crudHandler, "/"+e.GetTable(), crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
	}

	// MCP tools — namespaced if the group has a namespace. Explicit
	// MCP=true, or dev-implied for CRUD-enabled entities (the dev loop
	// gives the local agent the data tools without per-entity opt-in).
	if (config.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil {
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
			if err := a.MCP.RegisterTool(toolName, description, openapi.EndpointInputSchema(endpoint), endpoint.MCPHandler); err != nil {
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
		Registry:      NewRegistry(),
		router:        router.New(),
		MCP:           mcp.NewServer(),
		Config:        AppConfig{JSONCase: crud.CaseCamel},
		Plugins:       NewPluginManager(),
		Batteries:     NewBatteryManager(),
		events:        event.NewEventBus(),
		hooks:         make(map[string]*hook.HookRegistry),
		lc:            lifecycle.New(),
		startupOutput: os.Stdout,
	}

	for _, opt := range opts {
		opt(a)
	}
	// Resolve the process role once: WithRole wins, then GOFASTR_ROLE,
	// then RoleAll. An invalid value fails loudly — a typo'd role must
	// never silently run the wrong workload (a serve process that
	// quietly starts queue workers, or vice versa).
	role, err := resolveRole(a.roleOpt, a.roleSet)
	if err != nil {
		panic(err.Error())
	}
	a.role = role

	// Seed the App-local logger if no option supplied one. JSON to
	// stderr — independent of slog.Default so external slog rewiring
	// doesn't redirect this App's framework logs. battery/log replaces
	// it during Init via app.SetLogger.
	if a.logger.Load() == nil {
		a.logger.Store(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	// Construct the transactional outbox after all options have applied,
	// so option order between WithDB and WithOutbox doesn't matter.
	if len(a.outboxConsumers) > 0 && !a.outboxEnabled {
		// A declared consumer with no outbox would be silently dropped —
		// no durable delivery, no error. Fail loudly at construction.
		panic("framework: WithOutboxConsumer requires WithOutbox — enable the outbox for durable delivery")
	}
	if a.outboxEnabled {
		if a.DB == nil {
			panic("framework: WithOutbox requires WithDB — the outbox stages event rows in the entity write transaction")
		}
		ob, err := outbox.New(a.DB, a.outboxOpts...)
		if err != nil {
			panic("framework: WithOutbox: " + err.Error())
		}
		// Register declared durable consumers before the relay can start.
		// Consume panics on a duplicate (eventType, name) or nil handler,
		// surfacing the misconfiguration at construction rather than as a
		// silent no-op consumer.
		for _, c := range a.outboxConsumers {
			ob.Consume(c.name, c.eventType, c.handler)
		}
		a.outbox = ob
		a.outboxConsumers = nil // released; the registry owns them now
	}

	// Bridge the event bus to the fanout (WithFanout) at construction so
	// the real-time lane crosses replicas even for apps driven through
	// Router() without Start. AttachFanout can only fail on a double
	// attach (impossible here — one bridge per app) or a subscribe error
	// from the backend; both are construction-time misconfigurations.
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

	// Create the module manager (SQL store when DB is set, in-memory
	// otherwise) and wire the attribution hooks. The router register
	// hook stamps pattern→module during Init; the route gate returns 404
	// for disabled-module routes before the middleware chain runs. The
	// MCP register hook and call gate do the same for tools.
	a.modules = NewModuleManager(a.DB, a.fanout)
	a.router.SetRegisterHook(func(method, pattern string) {
		a.modules.recordRoute(method, pattern)
	})
	a.router.SetRouteGate(func(key string) bool {
		return a.modules.routeAllowed(key)
	})
	a.MCP.SetRegisterHook(func(toolName string) {
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
	if a.noDefaults && a.metrics != nil {
		panic("framework: WithMetrics is incompatible with WithoutDefaultMiddleware — " +
			"mount middleware.MetricsMiddleware + middleware.MetricsHandler in your custom chain instead")
	}
	if a.noDefaults && a.tracing {
		panic("framework: WithTracing is incompatible with WithoutDefaultMiddleware — " +
			"mount middleware.Tracing() in your custom chain instead")
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

	// Auto-enable the agent-facing MCP surface in the dev loop: /mcp
	// mount, read-only introspection, and runtime control — the dev
	// analogue of livereload, for agents instead of browsers. Same env
	// gate (GOFASTR_DEV, production GOFASTR_ENV wins), opt-out via
	// GOFASTR_DEV_MCP=0. Dev-implied flags are remembered so a host that
	// hand-mounted /mcp or registered same-named tools degrades to a
	// warning instead of the panic reserved for explicit double opt-in.
	if dev.DevMCPEnabled() {
		if !a.mcpAutoMount {
			a.mcpAutoMount, a.mcpMountDevImplied = true, true
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
// Returns the App for fluent chaining. Panics on any misconfiguration —
// convenient for static, hand-written declarations where a bad config is a
// programming error you want to fail fast on. For generated or untrusted
// configs (e.g. an AI-authored field, a dynamic schema) where one bad entity
// should not crash the process, use TryEntity, which returns the error.
func (a *App) Entity(name string, config entity.EntityConfig) *App {
	if err := a.TryEntity(name, config); err != nil {
		panic("framework: " + err.Error())
	}
	return a
}

// TryEntity is the error-returning variant of Entity: it registers an entity
// and returns an error on any misconfiguration instead of panicking. It also
// recovers panics from deeper validation (e.g. an invalid TenantField) and
// converts them to errors, so a single bad config can never take down the
// process — the property an agent-driven authoring loop needs.
func (a *App) TryEntity(name string, config entity.EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("entity %q: %v", name, r)
		}
	}()

	// Attribute the entity to the module whose Init is running (if any).
	if a.modules != nil {
		a.modules.recordEntity(name)
	}

	// Registration-time validation: SeedFS without SeedPath is a
	// misconfiguration that would otherwise silently mark the entity
	// as seeded with empty data on first run.
	if config.SeedFS != nil && config.SeedPath == "" {
		return fmt.Errorf("entity %q has SeedFS set but SeedPath empty — point SeedPath at a file within the FS or unset SeedFS", name)
	}

	e := entity.Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	if err := a.Registry.Register(e); err != nil {
		return fmt.Errorf("failed to register entity %q: %w", name, err)
	}

	// Auto-register CRUD routes.
	// Default (CRUD==nil): auto-register when DB is set.
	// Set CRUD to &true to always register, &false to opt out.
	// MCP=true implies CRUD must be mounted: MCP tools dispatch through the
	// router so they share its middleware chain (auth, recovery, etc.).
	crudEnabled := a.DB != nil && (config.CRUD == nil || *config.CRUD)
	if config.MCP && a.DB != nil && config.CRUD != nil && !*config.CRUD {
		return fmt.Errorf("entity %q has MCP=true with CRUD=false — MCP CRUD tools require the HTTP routes to be registered", name)
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		mountPath := a.entityMountPath(e.GetTable())
		// Pre-flight collision check: if a screen/route already owns this
		// entity's URL space, surface an actionable diagnostic that names
		// the entity, the path, and the fix — BEFORE the mux panics on the
		// opaque "/foods/llm.md conflicts with pattern" duplicate.
		if msg := a.entityRouteCollision(name, mountPath); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		crudHandler = crud.NewCrudHandler(e, a.DB)
		crudHandler.JSONCase = a.JSONCasing()
		crudHandler.Hooks = a.HookRegistry(name)
		crudHandler.Storage = a.Storage
		crudHandler.Events = a.Events()
		// Guarded assignment: a bare `= a.outbox` would wrap a typed nil
		// in the EventOutbox interface and silently swallow every event.
		if a.outbox != nil {
			crudHandler.Outbox = a.outbox
		}
		crudHandler.Registry = a.Registry
		// MCP tools dispatch in-process against a.router, where these routes are
		// mounted under the API prefix — tell the handler so its tool paths match.
		crudHandler.BasePath = a.apiPrefix()
		crud.RegisterCrudRoutes(a.router, crudHandler, mountPath, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
	}

	// Explicit MCP=true, or dev-implied: in the dev loop every
	// CRUD-enabled entity serves its MCP data tools so the local agent
	// can read AND write app data without per-entity opt-in. Production
	// keeps the explicit flag as the only path.
	if (config.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil {
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, a.router); err != nil {
			return fmt.Errorf("failed to register MCP tools for entity %q: %w", name, err)
		}
	}

	if len(config.Endpoints) > 0 {
		if err := a.registerEntityEndpoints(e, config.Endpoints); err != nil {
			return fmt.Errorf("failed to register endpoints for entity %q: %w", name, err)
		}
	}

	return nil
}

// CrudHandler returns a fully-wired in-process CRUD handler for a registered
// entity — the same handler shape the HTTP routes use (hooks, events, storage,
// JSON casing, registry). Use it to call CreateOne/UpdateOne/DeleteOne/ListAll
// directly, e.g. to compose several writes inside App.InTx (pass the InTx ctx
// so they join the same transaction). Returns an error if no entity is
// registered under name or the app has no DB.
func (a *App) CrudHandler(name string) (*crud.CrudHandler, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("app.CrudHandler: no DB configured")
	}
	ent, err := a.Registry.Get(name)
	if err != nil {
		return nil, fmt.Errorf("app.CrudHandler: %w", err)
	}
	ch := crud.NewCrudHandler(ent, a.DB)
	ch.JSONCase = a.JSONCasing()
	ch.Hooks = a.HookRegistry(name)
	ch.Storage = a.Storage
	ch.Events = a.Events()
	if a.outbox != nil {
		ch.Outbox = a.outbox
	}
	ch.Registry = a.Registry
	return ch, nil
}

// MustCrudHandler is CrudHandler that panics on error — for app setup where a
// missing entity is a programming mistake.
func (a *App) MustCrudHandler(name string) *crud.CrudHandler {
	ch, err := a.CrudHandler(name)
	if err != nil {
		panic(err)
	}
	return ch
}

// Table registers a raw, non-entity table for migration only — no CRUD, no
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
// plain+dialect Up collision). A loader error panics at registration time —
// mirroring App.View's misconfig panic — so the exact embed-path/file error
// surfaces in the console instead of shipping a half-loaded routine set.
func (a *App) RoutinesFS(fsys fs.FS, dir string) *App {
	rs, err := migrate.RoutinesFS(fsys, dir)
	if err != nil {
		panic(fmt.Sprintf("framework: App.RoutinesFS(%q): %v", dir, err))
	}
	a.migrationRoutines = append(a.migrationRoutines, rs...)
	return a
}

// View registers a database view — a virtual table built from other entities.
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
	if a.DB != nil {
		ent.SetDB(a.DB)
	}
	if err := a.Registry.Register(ent); err != nil {
		panic(fmt.Sprintf("framework: failed to register view %q: %v", v.Name, err))
	}
	if a.DB != nil {
		ch := crud.NewCrudHandler(ent, a.DB)
		ch.JSONCase = a.JSONCasing()
		ch.Registry = a.Registry
		crud.RegisterCrudRoutes(a.router, ch, a.entityMountPath(ent.GetTable()),
			crud.CrudRouteOptions{ReadOnly: true, NoLLMMD: a.Config.NoLLMMD})
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

// entityRouteCollision reports an actionable diagnostic when the entity
// named `name` would mount CRUD routes at a path already owned by another
// registration (typically a UI screen registered at the same name). It
// returns "" when there is no collision.
//
// The auto-CRUD mount registers GET <mountPath>, GET <mountPath>/{id},
// GET <mountPath>/llm.md, etc. Without this check the first overlap panics
// deep in net/http's ServeMux — usually on /llm.md — with a message that
// points at the generated doc handler rather than the underlying name
// clash. We catch the most common overlaps (the bare path and its /llm.md
// doc route) and explain WHAT collided and HOW to fix it.
func (a *App) entityRouteCollision(name, mountPath string) string {
	mountPath = strings.TrimRight(mountPath, "/")
	if mountPath == "" {
		return ""
	}
	// The paths auto-CRUD claims that a hand-registered screen most often
	// already owns. {id}/_batch/_events live in entity-only territory.
	claimed := map[string]bool{
		mountPath:             true,
		mountPath + "/llm.md": true,
	}
	for _, rt := range a.router.Routes() {
		if claimed[rt.Pattern] {
			return fmt.Sprintf(
				"entity %q would mount CRUD routes at %s (REST + %s/llm.md), "+
					"but a screen/route is already registered at %q. "+
					"Choose a different page path (e.g. /library, /library/{slug}), "+
					"rename the entity table, or move entity CRUD under an APIPrefix "+
					"(framework.WithAPIPrefix(\"/api\")) so the URL spaces don't collide.",
				name, mountPath, mountPath, rt.Pattern)
		}
	}
	return ""
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
			if err := a.MCP.RegisterTool(name, description, openapi.EndpointInputSchema(endpoint), endpoint.MCPHandler); err != nil {
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
	// Load module enable/disable state from the store into the in-memory
	// cache. Modules absent from the store default to enabled.
	if err := a.modules.loadFromStore(context.Background()); err != nil {
		a.initialized.Store(false)
		return err
	}
	// Probe both plugins and batteries for the optional
	// ReadinessRegistrar interface so they can publish health checks
	// before /readyz mounts in Start.
	a.probeReadinessRegistrars()

	// Register introspection MCP tools if opted in. After plugin/battery
	// init so app_plugins / app_batteries reflect everything. Dev-implied
	// registration (see NewApp) downgrades collisions with host-registered
	// tool names to a warning — dev must never fail an app that boots fine
	// in production.
	if a.mcpIntrospection {
		if err := a.registerIntrospectionTools(); err != nil {
			if !a.mcpIntrospectionDevImplied {
				return err
			}
			a.Logger().Warn("dev MCP introspection tools partially skipped", "error", err)
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

// OnReady registers a function to run once the HTTP listener has bound
// successfully, just before the server begins accepting connections. The
// addr passed in is the listener's resolved address (a ":0" request
// arrives with the real port), so it is safe to print in a startup
// banner: every earlier phase — auto-migrate, seeds, plugin init, OnStart
// hooks, and the bind itself — has already succeeded. Hooks run in
// registration order and must not block.
func (a *App) OnReady(fn func(addr string)) *App {
	a.readyHooks = append(a.readyHooks, fn)
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
//
// The stop side drains through StopContext so in-flight job goroutines
// are joined before shutdown proceeds — bounded by the drain deadline,
// so a job that ignores its (already-cancelled) context can't hang
// SIGTERM forever.
//
// Worker-scoped: under RoleServe this is a no-op — neither the start hook
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
func (a *App) runStartHooks() error {
	a.ensureLifecycleContext()

	// Start batteries in dependency order
	if err := a.Batteries.StartAll(a.appCtx); err != nil {
		return err
	}

	// Start the process-module supervisor's per-module loops (issue #37).
	// Each registered module's supervise goroutine launches here, after
	// InitPlugins has loaded module state. The supervisor's drain drainer
	// is registered via PrependDrainer so children drain FIRST, before
	// the DB/queue batteries they may need for in-flight reverse calls
	// (design §8). Idempotent via processDrainRegistered.
	if a.processModules != nil {
		a.processModules.StartLoops()
		if !a.processDrainRegistered {
			a.processDrainRegistered = true
			if err := a.lc.PrependDrainer(a.processModules); err != nil {
				a.Logger().Warn("process module drain drainer not registered", "error", err)
			}
		}
	}

	// Then app-level start hooks (cron, queues, custom)
	for _, fn := range a.startHooks {
		if err := fn(a.appCtx); err != nil {
			return err
		}
	}
	return nil
}

// Worker-scoped: under RoleServe this is a no-op — neither the start hook
// nor the Close hook is registered, so a serve-only shutdown never closes
// a queue it never started.
// schedulerStartStop is the minimal interface AddQueue needs. We keep it
// here (not in the queue package) so framework doesn't have to import
// battery/queue — apps wire their queue manually and just hand the
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
	// Snapshot the cancel func and detach the server under the same lock
	// that Start/runStartHooks use to assign them. Invoke cancel OUTSIDE
	// the lock so a cancel-triggered teardown can't re-enter and deadlock.
	var firstErr error
	a.serverMu.Lock()
	cancel := a.appCancel
	a.appCancel = nil
	srv := a.server
	a.server = nil
	a.serverMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			// Bounded drain: the deadline expired with connections still
			// open (an SSE stream never goes idle, so Server.Shutdown
			// alone would leave it — and the process — hanging).
			// Force-close the stragglers so shutdown completes.
			_ = srv.Close()
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

// ensureLifecycleContext lazily creates the app's cancellable lifecycle
// context under serverMu — the same lock that guards server. Start and
// runStartHooks both call this before binding the port, and Shutdown
// reads/nils appCancel under the same lock, so a SIGTERM-driven Shutdown
// racing pre-listen setup is data-race free.
func (a *App) ensureLifecycleContext() {
	a.serverMu.Lock()
	if a.appCtx == nil {
		a.appCtx, a.appCancel = context.WithCancel(context.Background())
	}
	a.serverMu.Unlock()
}

// Start starts the HTTP server on the given address.
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
	a.ensureLifecycleContext()

	abort := func(err error) error {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = a.Shutdown(shutdownCtx)
		return err
	}

	// Auto-migrate all registered entities — unless the deployment opted
	// into explicit migrations only (WithoutAutoMigrate). Seeds run
	// either way: they are idempotent data writes, not schema, and a
	// seeded entity whose table is missing fails Start here rather than
	// serving against an unmigrated schema.
	if a.DB != nil {
		if !a.noAutoMigrate {
			plan := migrate.Plan{Registry: a.Registry, Views: a.migrationViews, Routines: a.migrationRoutines}
			if err := migrate.AutoMigratePlanContext(a.appCtx, a.DB, plan); err != nil {
				return abort(fmt.Errorf("auto-migrate: %w", err))
			}
		}
		if err := migrate.RunSeeds(a.appCtx, a.DB, a.Registry); err != nil {
			return abort(fmt.Errorf("run seeds: %w", err))
		}
	}

	// App-level seed hooks (App.WithSeed). Run after auto-migration + the
	// per-entity RunSeeds phase so every table exists, and before plugins
	// init so a plugin that reads seed data sees it.
	if err := a.runSeedHooks(); err != nil {
		return abort(fmt.Errorf("seed hooks: %w", err))
	}

	// Initialize plugins and batteries (routes, middleware, tools, hooks).
	// Must happen after auto-migrate (so entity tables exist) but before
	// start hooks (so batteries can reference their routes).
	if err := a.InitPlugins(); err != nil {
		return abort(fmt.Errorf("init plugins: %w", err))
	}
	// Check first-run setup status (WithSetup). When incomplete and not
	// disabled via GOFASTR_SETUP=off, Start either runs the steps inline
	// (headless: every required env present) or defers consumer startup
	// and serves the interactive wizard until the final step completes.
	// Worker role + incomplete setup is a hard error — setup touches
	// tables the worker's consumers would race on.
	setupInteractive := false
	var setupURL string
	if a.setup != nil {
		mode, err := resolveSetupEnv()
		if err != nil {
			return abort(fmt.Errorf("setup: %w", err))
		}
		if mode != setupOff {
			incomplete, err := a.setup.Incomplete(a.appCtx)
			if err != nil {
				return abort(fmt.Errorf("setup: %w", err))
			}
			if mode == setupForce || incomplete {
				if a.role == RoleWorker {
					return abort(fmt.Errorf("setup incomplete: run a serve/all process to complete setup first"))
				}
				canHeadless, err := a.setup.CanRunHeadless(a.appCtx)
				if err != nil {
					return abort(fmt.Errorf("setup: %w", err))
				}
				if canHeadless {
					if err := a.setup.RunSteps(a.appCtx); err != nil {
						return abort(fmt.Errorf("setup: %w", err))
					}
					// Headless bootstrap finished — proceed normally.
				} else {
					setupInteractive = true
				}
			}
		}
	}

	// Run OnStart hooks (cron/queue workers, custom setup). Failure here
	// aborts before we bind the port — better than a half-up server.
	// Deferred when interactive setup is active: the hooks may start
	// consumers that touch tables setup owns. They run inside the swap
	// callback once setup completes.
	if !setupInteractive {
		if err := a.runStartHooks(); err != nil {
			return abort(fmt.Errorf("start hooks: %w", err))
		}
	}

	// Start the outbox relay (WithOutbox). Runs on the app lifecycle
	// context so Shutdown cancels it; the returned stop func is also
	// registered as an OnStop drainer so shutdown blocks until the loop
	// has fully exited — no half-delivered batch outlives the process.
	// Worker-scoped: a serve-only process stages rows (StageEvent runs in
	// the write transaction regardless of role) but never claims them —
	// delivery belongs to the worker/all processes.
	// Deferred when interactive setup is active (started in the swap).
	if a.outbox != nil && a.runsWorkers() && !setupInteractive {
		stopRelay := a.outbox.StartRelay(a.appCtx)
		a.OnStop(func() error {
			stopRelay()
			return nil
		})
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
		spec := openapi.EntityOpenAPI(a.Registry, appName, "1.0.0", a.apiPrefix())
		if a.Config.PublicOpenAPI {
			a.router.Get("/openapi.json", coreoa.PublicHandler(spec))
		} else {
			a.router.Get("/openapi.json", coreoa.Handler(spec))
		}
		a.router.Get("/api/docs/", coreoa.SwaggerUIHandler(spec, "/api/docs"))

		// API entity index under /api/ alongside /api/docs/ (Swagger).
		// Root /llm.md is free for the homepage screen doc.
		if !a.Config.NoLLMMD {
			a.router.Get("/api/llm.md", crud.RegistryLLMMDHandler(a.Registry, appName))
			hasLLMMD = true
		}
	}

	// Prometheus /metrics endpoint when metrics are enabled (WithMetrics).
	// Unauthenticated by design — scrape from inside the network boundary.
	if a.metrics != nil {
		a.router.Get("/metrics", middleware.MetricsHandler(a.metrics))
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
		// scaffolds mount POST /mcp themselves) — dev must not turn a
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
	if a.mcpAutoMount {
		a.router.Get("/mcp/server-card", http.HandlerFunc(a.handleMCPServerCard))
		a.router.Get("/.well-known/mcp/catalog.json", http.HandlerFunc(a.handleMCPCatalog))
	}
	// OAuth Protected Resource metadata (RFC 9728). Opt-in; advertises how
	// OAuth-token-protected resources accept tokens.
	if a.oauthResource != nil {
		a.router.Get("/.well-known/oauth-protected-resource", http.HandlerFunc(a.handleOAuthProtectedResource))
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

	a.serverMu.Lock()
	// Resolve the real handler (full router for all/serve, health mux
	// for worker). When interactive setup is active, the server delegates
	// to whatever handlerCell currently points at — initially the setup
	// wizard, swapped to realHandler by the swap callback on completion.
	realHandler := a.roleHandler()
	serveHandler := realHandler
	if setupInteractive {
		var swapOnce sync.Once
		swap := func() {
			swapOnce.Do(func() {
				// Serve the real router first: setup IS complete (the
				// runner only swaps after its Complete predicate flips),
				// so the app must never keep answering 503 from here on.
				a.handlerCell.Store(&servingHandler{h: realHandler})
				// Start the deferred consumers. A failure here gets the
				// same fail-loud semantics as the identical failure at
				// normal boot (which aborts Start): log and shut down —
				// the process exits, and the NEXT boot finds setup
				// complete and runs the hooks on the normal Start path,
				// failing Start properly. Silently running without
				// consumers would be a half-up server.
				if err := a.runStartHooks(); err != nil {
					a.Logger().Error("setup: deferred start hooks failed; shutting down (restart will boot normally and surface this error at Start)", "error", err)
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						_ = a.Shutdown(ctx)
					}()
					return
				}
				if a.outbox != nil && a.runsWorkers() {
					stopRelay := a.outbox.StartRelay(a.appCtx)
					a.OnStop(func() error {
						stopRelay()
						return nil
					})
				}
			})
		}
		liveness, readiness := a.healthHandlers()
		a.handlerCell.Store(&servingHandler{h: a.setup.Handler(swap, liveness, readiness)})
		serveHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cell := a.handlerCell.Load(); cell != nil {
				cell.h.ServeHTTP(w, r)
			} else {
				realHandler.ServeHTTP(w, r)
			}
		})
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           serveHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	a.server = srv
	a.serverMu.Unlock()
	// Bind first, then Serve — split from ListenAndServe so OnReady hooks
	// fire only after the port is actually held. http.ListenAndServe
	// defaults an empty Addr to ":http"; net.Listen needs that explicit.
	listenAddr := addr
	if listenAddr == "" {
		listenAddr = ":http"
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// Bind failure (port in use is the common case) — drain like every
		// earlier start phase does, otherwise the batteries/cron/queue and
		// OnStart workers spawned above leak past Start returning.
		return abort(fmt.Errorf("listen and serve: %w", err))
	}
	// Serve closes the listener on Shutdown; this extra Close only matters
	// on the Shutdown-raced path where Serve returns without adopting ln.
	defer func() { _ = ln.Close() }()

	// Graceful shutdown by default: docker stop and kubectl rollouts send
	// SIGTERM, and without a handler the runtime kills the process
	// mid-request — no drain, no battery stop, no OnStop hooks. The
	// deferred join keeps Start from returning while a signal-triggered
	// drain is still running (Serve returns ErrServerClosed as soon as
	// Shutdown begins). Opt out via DisableSignalHandling when the host
	// process owns signals and calls Shutdown/RunWithSignals itself;
	// concurrent Shutdown calls are safe — it is idempotent.
	if !a.Config.DisableSignalHandling {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sigQuit := make(chan struct{})
		sigDone := make(chan struct{})
		go func() {
			defer close(sigDone)
			defer signal.Stop(sigCh)
			select {
			case sig := <-sigCh:
				timeout := a.Config.ShutdownTimeout
				if timeout <= 0 {
					timeout = defaultShutdownTimeout
				}
				a.Logger().Info("shutdown signal received; draining",
					"signal", sig.String(), "timeout", timeout.String())
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				if err := a.Shutdown(ctx); err != nil {
					a.Logger().Error("graceful shutdown incomplete", "error", err)
				}
			case <-sigQuit:
			}
		}()
		defer func() {
			close(sigQuit)
			<-sigDone
		}()
	}

	if setupInteractive {
		setupURL = a.setup.SetupURL(ln.Addr().String())
	}
	a.printStartupBanner(ln.Addr().String(), name, hasAPI, hasLLMMD, setupURL)
	for _, fn := range a.readyHooks {
		fn(ln.Addr().String())
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return abort(fmt.Errorf("listen and serve: %w", err))
	}
	return nil
}

// printStartupBanner reports readiness only after the listener has bound.
// boundAddr is the resolved listener address, so Start("127.0.0.1:0") prints
// the actual port and a bind failure prints nothing.
func (a *App) printStartupBanner(boundAddr, name string, hasAPI, hasLLMMD bool, setupURL string) {
	w := a.startupOutput
	if w == nil {
		w = os.Stdout
	}
	if a.role == RoleWorker {
		// The worker serves only the health surface — advertising entity or
		// API routes here would advertise 404s.
		fmt.Fprintf(w, "\n  %s %s worker ready\n", bold("GoFastr"), name)
		fmt.Fprintf(w, "  %s PID: %d\n", arrow(), os.Getpid())
		fmt.Fprintf(w, "  %s Health: http://%s/healthz http://%s/readyz\n", arrow(), boundAddr, boundAddr)
		_, _ = fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "\n  %s %s server ready\n", bold("GoFastr"), name)
	fmt.Fprintf(w, "  %s PID: %d\n", arrow(), os.Getpid())
	fmt.Fprintf(w, "  %s Listening: http://%s\n", arrow(), boundAddr)
	if setupURL != "" {
		fmt.Fprintf(w, "  %s Setup: %s\n", arrow(), setupURL)
	}
	if a.Config.DebugEndpoints {
		fmt.Fprintf(w, "  %s Stats: http://%s/.debug/stats\n", arrow(), boundAddr)
	}

	for _, e := range a.Registry.All() {
		fmt.Fprintf(w, "  %s %-12s http://%s%s\n", arrow(), e.GetName(), boundAddr, a.entityMountPath(e.GetTable()))
	}

	if hasAPI {
		fmt.Fprintf(w, "  %s OpenAPI:     http://%s/openapi.json\n", arrow(), boundAddr)
		fmt.Fprintf(w, "  %s Swagger UI:  http://%s/api/docs/\n", arrow(), boundAddr)
		if hasLLMMD {
			fmt.Fprintf(w, "  %s LLM Docs:    http://%s/api/llm.md\n", arrow(), boundAddr)
		}
	}
	_, _ = fmt.Fprintln(w)
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
