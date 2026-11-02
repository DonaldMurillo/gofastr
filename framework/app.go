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
	"os/signal"
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
	"syscall"
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
	"github.com/DonaldMurillo/gofastr/internal/retired"
)

// Mountable is anything that can register routes on the framework's router.
// UI hosts, admin panels, websocket pubsub layers, etc. all satisfy this
// interface and are attached via App.Mount.
type Mountable interface {
	Mount(*router.Router)
}

// JSONCase / CaseCamel / CaseSnake moved to framework/crud. See
// reexports_crud.go for the facade aliases that keep framework.X working.

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name           string        // application name
	JSONCase       crud.JSONCase // JSON key casing: "camelCase" (default) or "snake_case"
	DebugEndpoints bool          // opt-in for /.debug/* endpoints
	NoLLMMD        bool          // disable auto-generated /llm.md entity docs

	// PublicOpenAPI serves /openapi.json without the auth gate. By default
	// the spec is auth-gated (it enumerates every route), so a minimal app
	// returns 401 there, which surprised users following the quickstart
	// curl. Set true (or use WithPublicOpenAPI) when the spec is meant to be
	// public, e.g. a docs site or an internal API behind a network boundary.
	// The API docs page at /api/docs/ follows the same setting: public
	// spec, public browse page; gated spec, gated page.
	PublicOpenAPI bool

	// APIPrefix mounts every auto-CRUD entity route (list/get/create/update/
	// delete + _batch + _events + per-entity llm.md) under this path, e.g.
	// "/api" serves GET /api/posts instead of GET /posts. Empty (default)
	// keeps the bare entity-name mounts, so this is not a breaking change.
	// The generated OpenAPI spec bakes the prefix into its path keys
	// (servers stays [{url: "/"}]), so documented paths match requests.
	// GroupEntity routes are unaffected. A group owns its own prefix. MCP
	// tool names are unchanged.
	APIPrefix string

	// RequestTimeout caps the per-request wall-clock budget enforced by
	// the default middleware chain. Zero (default) installs a 30s cap.
	// Set a positive duration to override. The cap applies to buffered
	// responses only: a handler that flips into streaming mode (first
	// Flush or Hijack, every SSE subscription does) sheds the deadline
	// and the server-level read/write deadlines for the life of the
	// stream, so subscribers are never cut off by this budget. To
	// disable the timeout middleware entirely, set
	// DisableRequestTimeout. Overloading sign for "disable" is too
	// easy to trip on (e.g. accidentally subtracting two timestamps).
	RequestTimeout time.Duration

	// DisableRequestTimeout removes the Timeout middleware from the
	// default chain entirely and drops the server-level read/write
	// timeouts to match (header and idle timeouts stay). Useful for
	// long-running uploads; SSE no longer needs it. Streams shed the
	// deadline at their first flush. Pair with per-handler ctx
	// deadlines if you still need bounded request lifetime.
	DisableRequestTimeout bool

	// Compression adds the Compress middleware to the default chain:
	// text responses (HTML, CSS, JavaScript, JSON) are gzipped for
	// clients that accept gzip; see middleware.Compress for what is
	// skipped. Off by default, since a reverse proxy or CDN in front of
	// the app usually compresses; set it when the app faces clients
	// directly.
	Compression bool

	// HTTPServerTimeouts overrides the four connection deadlines on the
	// underlying http.Server: ReadHeaderTimeout, ReadTimeout,
	// WriteTimeout, IdleTimeout. Every field is a *time.Duration so an
	// unset field (nil) keeps its framework default while an explicit
	// pointer to 0 disables that one timeout (net/http semantics: 0
	// means no timeout). A plain time.Duration could not tell those two
	// apart. Defaults: read-header 10s, read 60s, write 60s, idle 120s.
	// See [HTTPServerTimeoutsConfig] and [WithHTTPServerTimeouts].
	//
	// WriteTimeout bounds the whole response, including an SSE stream:
	// when it lapses the browser's EventSource reconnects automatically, so
	// the single /__gofastr/sse bus keeps working at any value. To hold
	// one stream open for longer (fewer reconnects on a presence/collab
	// surface), raise WriteTimeout or set it to 0. See the deployment
	// guide's "HTTP server timeouts" section.
	HTTPServerTimeouts HTTPServerTimeoutsConfig

	// TLS serves HTTPS on the listen address. Nil (the default) serves
	// plain HTTP, the shape behind a proxy that terminates TLS. See
	// [TLSConfig] and [WithTLS].
	TLS *TLSConfig

	// HTTPProtocols sets the HTTP versions the server speaks, passed to
	// http.Server.Protocols. Nil keeps net/http's default: HTTP/1.1, plus
	// HTTP/2 when TLS is on. SetUnencryptedHTTP2 turns on HTTP/2 without
	// TLS (h2c with prior knowledge, no Upgrade), for a proxy that speaks
	// h2c to the app. HTTP/3 is not offered. See [WithHTTPProtocols].
	HTTPProtocols *http.Protocols

	// SecurityHeaders configures the defensive HTTP response headers
	// (Content-Security-Policy, Referrer-Policy, X-Frame-Options,
	// Permissions-Policy, CORP, COOP, HSTS) emitted by the SecurityHeaders
	// middleware in the default chain. The zero value installs the
	// framework's strict defaults (default-src 'self', no 'unsafe-inline');
	// set fields to override, e.g. ContentSecurityPolicy to allow
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

// HTTPServerTimeoutsConfig overrides the connection deadlines of the
// framework's embedded http.Server. Each field mirrors the equally named
// field on [net/http.Server]. Fields are *time.Duration (not time.Duration)
// so an unset field, nil, keeps the framework default, while an explicit
// pointer to 0 disables just that deadline (matching net/http, where a zero
// time.Duration means "no timeout"). Build the pointers with the Go 1.26
// new(expr) builtin:
//
//	app := framework.NewApp(framework.WithHTTPServerTimeouts(framework.HTTPServerTimeoutsConfig{
//	    WriteTimeout: new(2 * time.Minute), // raise for slow handlers
//	    ReadTimeout:  new(time.Duration(0)), // disable (e.g. huge uploads)
//	}))
//
// ReadHeaderTimeout bounds reading the request headers (slowloris defence)
// and is always kept non-zero by the defaults; prefer leaving it set.
type HTTPServerTimeoutsConfig struct {
	// ReadHeaderTimeout is http.Server.ReadHeaderTimeout.
	ReadHeaderTimeout *time.Duration
	// ReadTimeout is http.Server.ReadTimeout.
	ReadTimeout *time.Duration
	// WriteTimeout is http.Server.WriteTimeout. It bounds the whole
	// response write, including a streaming/SSE response: when it lapses
	// the client's EventSource reconnects, so SSE keeps working at any
	// value. Set to 0 to disable it for long-lived single streams.
	WriteTimeout *time.Duration
	// IdleTimeout is http.Server.IdleTimeout.
	IdleTimeout *time.Duration
}

// Defaults applied to the http.Server when AppConfig.HTTPServerTimeouts leaves
// a field unset. ReadHeaderTimeout is kept small as a slowloris guard;
// Read/WriteTimeout bound ordinary request/response cycles; IdleTimeout lets a
// keep-alive connection sit between requests. See AppConfig.DisableRequestTimeout
// for the coarse opt-out that zeroes Read/Write (keeping these as the fallback).
const (
	defaultServerReadHeaderTimeout = 10 * time.Second
	defaultServerReadTimeout       = 60 * time.Second
	defaultServerWriteTimeout      = 60 * time.Second
	defaultServerIdleTimeout       = 120 * time.Second
)

// App is the top-level application container.
// It wires together the entity registry, router, MCP server, and database.
type App struct {
	Registry            *Registry
	router              *router.Router // access via App.Router() method
	MCP                 *mcp.Server
	DB                  *sql.DB
	Config              AppConfig
	Plugins             *PluginManager
	Storage             upload.Storage                          // optional; enables multipart on Image/File fields
	imageDeriver        file.ImageDeriver                       // optional; derives renditions + BlurHash for Image fields
	fieldImageDerivers  map[string]map[string]file.ImageDeriver // entity -> field -> override
	stripUploadMetadata bool                                    // WithStripUploadMetadata: strip EXIF/XMP from stored originals

	// auditTable is the audit log's table once WithAuditLog ran; "" when
	// the app keeps no audit log. entityui's Activity tab reads it.
	auditTable string
	// auditActor is WithAuditLog's actor resolver, for audit rows written
	// outside the CRUD hooks (entityui's bulk runs).
	auditActor func(context.Context) string
	// auditCfg is WithAuditLog's config, for the Activity tab to map a
	// record id to the record_id the audit rows store (AuditConfig.keyID).
	auditCfg *AuditConfig
	// entityUI is the UI EntityUI built; a second call is refused.
	entityUI *entityui.UI
	// pendingEndpoints holds EntityEndpoints calls until App.Entity
	// registers the entity they name.
	pendingEndpoints map[string][]entity.Endpoint
	// crudMounts records where each entity's write routes mounted: the
	// router that carries them (a group's sub-router, with its
	// middleware) and the path on it. EntityUI mounts the bulk and export
	// routes on the same router, and its screens post to the full path.
	crudMounts map[*entity.Entity]crudMount

	migrationRoutines []migrate.Routine // stored procedures/functions/triggers run on boot
	migrationViews    []migrate.View    // views (virtual tables built from entities) run on boot

	Batteries *BatteryManager

	// embedHost is the mounted embeddable-surface host, if any. Captured at
	// Mount so InitPlugins can register the reserved prefixes of whatever
	// batteries this app actually mounted.
	embedHost *fembed.Host

	// modules manages registered modules and their enable/disable state.
	// Created in NewApp; nil-safe (NewModuleManager always returns non-nil).
	modules *ModuleManager

	// access, when set (WithGrantStore), is the app's RBAC GrantStore.
	// When a fanout is also attached (WithFanout), grant/revoke propagate
	// to every replica as a refresh-signal. Nil by default. Apps that
	// don't use RBAC are unaffected.
	access *access.GrantStore

	// processModules supervises out-of-process third-party modules
	// (issue #37, wave 2a). Lazily constructed by RegisterProcessModule;
	// nil when the app has no process modules. [App.Start] starts its
	// loops and [App.Shutdown] drains it.
	processModules *ProcessModuleSupervisor

	// moduleTools is the MCP tool registry shared across every process
	// module (design §5.1). Lazily constructed alongside processModules;
	// nil when the app has no process modules. The supervisor uses it as
	// its ToolRegistrar; the composite MCP call gate consults it so a
	// disabled module's tools are omitted from tools/list.
	moduleTools *ModuleToolRegistry

	serverMu sync.Mutex // guards server + appCtx/appCancel. Start writes, Shutdown reads/nils
	server   *http.Server
	// shutdownDone is set by the first Shutdown and closed when its
	// teardown returns; every later call waits on it instead of tearing
	// down again. Guarded by shutdownMu.
	shutdownMu   sync.Mutex
	shutdownDone chan struct{}
	// newConns holds connections the server accepted that have not sent
	// a request yet (net/http's StateNew): a browser's speculative
	// preconnect, a Transport's spare dial. Server.Shutdown spares such a
	// connection for five seconds (go.dev/issue/22682), which turns a
	// bounded drain into a five-second stall, so Shutdown closes them
	// itself first. The ConnState hook keeps the set exact.
	newConnsMu sync.Mutex
	newConns   map[net.Conn]struct{}
	// streams holds the open event-stream requests, which Shutdown ends
	// when the drain starts (shutdown_streams.go).
	streams    openStreams
	events     *event.EventBus
	hooksMu    sync.RWMutex // guards hooks: kiln's build-mode runtime registers while the app serves
	hooks      map[string]*hook.HookRegistry
	mountables []Mountable
	noDefaults bool

	// outbox is the transactional event outbox (WithOutbox). When set,
	// CRUD lifecycle events are staged in the write transaction and a
	// relay, started by Start, drained on Shutdown, delivers each to
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
	// Mount time. Caller-owned. The app never closes it.
	fanout fanout.Fanout

	// secret is the app-wide secret (WithSecret, or GOFASTR_SECRET).
	// Subsystem keys are HKDF-derived from it, never used raw, starting
	// with the uihost session-signing key wired at Mount time. Empty is
	// valid for a single replica (the host self-mints a per-boot key);
	// with a fanout attached (multi-replica) Mount fails closed instead,
	// because sessions minted on one replica must verify on every other.
	secret []byte
	// previousSecrets are retired app secrets accepted (verify-only) for
	// graceful GOFASTR_SECRET rotation, mirroring the CSRF AdditionalKeys
	// idiom. Their session-signing keys are handed to a mounted host
	// alongside the current key (SetSessionKeys); tokens signed by a
	// previous secret still verify for a drain window (one session TTL).
	// Set via WithSecretRotation or GOFASTR_SECRET_PREVIOUS.
	previousSecrets [][]byte

	// secretOptionSet records that WithSecret or WithSecretRotation ran, so
	// the GOFASTR_SECRET_PREVIOUS env fallback stays out of an explicitly
	// configured rotation window (including one explicitly emptied).
	secretOptionSet bool

	// noAutoMigrate suppresses the boot-time entity auto-migration
	// (WithoutAutoMigrate) for deployments that require every schema
	// change to be an explicit, operator-invoked step.
	noAutoMigrate bool

	// logger is the App-local *slog.Logger. Read via Logger(), swapped via
	// SetLogger(). Stored behind an atomic pointer so middleware composed
	// at NewApp time can resolve the current logger per request without a
	// lock, plugins (battery/log, etc.) can swap it from their Init.
	logger atomic.Pointer[slog.Logger]

	// initialized guards InitPlugins against double-init. Public so a test
	// or CLI can call InitPlugins manually pre-Start, then Start also calls
	// it. The second call must be a no-op (otherwise routes/middleware
	// double-register and panic on duplicate mux patterns).
	initialized atomic.Bool

	// Lifecycle hooks fired by Start/Stop. startHooks run with the app's
	// derived context so workers cancel when Stop is called.
	startHooks []func(ctx context.Context) error
	appCtx     context.Context
	appCancel  context.CancelFunc

	// readyHooks fire once the HTTP listener has bound, just before the
	// server starts accepting connections. See App.OnReady.
	readyHooks []func(addr string)

	// seedHooks run during Start AFTER auto-migration (tables exist) and
	// before plugin/battery init. See App.WithSeed.
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
	flagAccessed bool // true once Flags()/SetFlagStore has run. Guards against late SetFlagStore

	// Optional idempotency config wired into the default chain when set.
	idempotency *middleware.IdempotencyConfig

	// Optional translator. When set, the i18n middleware is wired into
	// the default chain so handlers can call App.T(ctx, key, ...).
	translator *i18n.Translator

	// Optional locale resolver (set via WithLocaleResolver). Consulted
	// before X-Locale / Accept-Language during negotiation so a stored
	// per-user locale (cookie) can win. Only effective with WithI18n.
	// Unset while its Resolve is nil.
	localeResolver i18n.LocaleResolver

	// localePrefixes is set by WithLocalePrefixes; pathLocales is the
	// locale set NewApp reads from the translator for it.
	localePrefixes bool
	pathLocales    i18n.PathLocales

	// Optional metrics. When set (via WithMetrics), the metrics middleware
	// joins the default chain and a Prometheus /metrics endpoint is mounted.
	metrics *middleware.Metrics

	// debugAuthorize replaces the admin-role check on /.debug/* (see
	// WithDebugAuthorize). nil means the default.
	debugAuthorize func(ctx context.Context) bool

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
	// mcpControlGate replaces the control tools' admin-role gate. Set via
	// WithMCPControlGate.
	mcpControlGate func(ctx context.Context) error
	// localRunInfo is SetLocalRunInfo's addition to the local run
	// record Start writes.
	localRunInfo LocalRunInfo
	// mcpRegistrars run against a.MCP during InitPlugins, after every
	// plugin and battery Init and before the introspection set. Set via
	// WithMCPTools.
	mcpRegistrars []func(*mcp.Server) error
	// mcpApps queues MCP App registrations (a UI resource + its linking
	// tool) added via WithMCPApp, registered during InitPlugins.
	mcpApps []mcp.AppConfig
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
	// mcpGate is WithMCPGate's precondition, held here and installed on
	// the final a.MCP once every option has run: set on the server at
	// option time, a later WithMCPServer swapped in an ungated one.
	mcpGate func(ctx context.Context) error
	// a2aCfg, when set via WithA2A, mounts the Agent2Agent task
	// exchange during Start. Held until then because the derived entity
	// skills read the MCP tool registry, complete only once entities,
	// plugins, and batteries have initialized.
	a2aCfg *A2AConfig
	// a2a is the mounted exchange server; nil until Start mounts it
	// (and forever when WithA2A was never called). See App.A2A().
	a2a *a2a.Server
	// a2aPath is the configured exchange path (default "/a2a"), read by
	// agentMux so the agent role forwards it the way it forwards /mcp.
	a2aPath string
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
	// authenticatorNoted is set by NoteAuthenticator; the startup banner
	// reads it to decide whether to print its "no auth is wired" line.
	authenticatorNoted atomic.Bool
	// webBotAuth/ucp/acp: opt-in commerce + bot-signing discovery docs
	// (isitagentready.com production checks). Set via WithWebBotAuth /
	// WithUCP / WithACP. webBotAuth.Verify additionally installs the
	// inbound Web Bot Auth verification middleware (experimental).
	webBotAuth         *WebBotAuthConfig
	webBotAuthVerifier *webbotauth.Verifier
	ucp                *UCPConfig
	acp                *ACPConfig

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

	// configDiscards collects AppConfig field names WithConfig zeroed after
	// an earlier option had set them. NewApp filters it against the final
	// Config (a later option may have restored the field) and warns once.
	// The discard is legal, replace-semantics are the contract, but it is
	// almost always a misplaced option and used to be perfectly silent.
	configDiscards []string

	// setup is the optional first-run setup runner (WithSetup). When set
	// and setup is incomplete at boot, Start either runs the steps
	// headlessly (all required env present) or serves an interactive
	// wizard until setup finishes, then atomically swaps to the real
	// router. See SetupRunner for the full lifecycle.
	setup SetupRunner

	// handlerCell holds the currently-serving http.Handler behind an
	// atomic.Pointer so the interactive-setup swap can switch from the
	// wizard surface to the real app router without a restart. We use a
	// wrapper struct (not atomic.Value) because atomic.Value panics when
	// the concrete type changes between Stores (http.HandlerFunc vs
	// *router.Router). The wrapper normalizes both to *servingHandler.
	handlerCell atomic.Pointer[servingHandler]

	// serverWrappers wrap the handler Start hands the http.Server, so
	// they see every request the listener accepts: the router, a role's
	// reduced mux, and the interactive setup wizard alike. Guarded by
	// serverMu. See WrapServer.
	serverWrappers []func(http.Handler) http.Handler
	// serverWrapped latches once Start has applied serverWrappers; a
	// later WrapServer panics even after Shutdown clears a.server.
	serverWrapped bool

	// txDialectVal caches the DB's dialect for InTx's read-only path;
	// txDialectMu guards it. Empty until a probe succeeds.
	txDialectMu  sync.Mutex
	txDialectVal migrate.Dialect
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

// WithConfig sets the application config by replacing AppConfig wholesale.
// Options apply left to right and later options win: a granular setter placed
// after WithConfig (WithAPIPrefix, WithPublicOpenAPI, …) overrides the field
// it touches, while WithConfig overrides every option that ran before it,
// including fields it leaves at the zero value. A zero field means the zero
// value, not "keep whatever a previous option set". To turn a boolean back off,
// set it in the AppConfig passed to WithConfig (or place a granular setter
// after it) instead of relying on a zero field to preserve a prior value.
//
// Because that discard used to be perfectly silent. The classic form is a
// scaffold that pastes WithPublicOpenAPI() next to WithDB, one line above the
// WithConfig that zeroes it. NewApp now logs a warning naming every field an
// earlier option set that the replacement returned to zero (and that no later
// option restored). Place WithConfig first to stay silent.
func WithConfig(config AppConfig) AppOption {
	return func(a *App) {
		a.configDiscards = append(a.configDiscards, discardedConfigFields(a.Config, config)...)
		a.Config = config
	}
}

// defaultAppConfig is the Config NewApp seeds before options run. It exists so
// the seed and discardedConfigFields' notion of "nothing was set yet" cannot
// drift: a seeded default (JSONCase) being replaced is business as usual, not
// a lost option.
func defaultAppConfig() AppConfig {
	return AppConfig{JSONCase: crud.CaseCamel}
}

// discardedConfigFields reports the AppConfig fields that old carried beyond
// the NewApp seed and replacement returns to zero: the fields a wholesale
// WithConfig replacement silently throws away.
func discardedConfigFields(old, replacement AppConfig) []string {
	def := reflect.ValueOf(defaultAppConfig())
	ov := reflect.ValueOf(old)
	nv := reflect.ValueOf(replacement)
	var out []string
	for i := 0; i < ov.NumField(); i++ {
		if !nv.Field(i).IsZero() {
			continue // the replacement sets it; nothing lost
		}
		if reflect.DeepEqual(ov.Field(i).Interface(), def.Field(i).Interface()) {
			continue // nothing beyond the seed to lose
		}
		out = append(out, ov.Type().Field(i).Name)
	}
	return out
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
// ContentSecurityPolicy) to relax a specific directive, unset fields
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

// WithHTTPServerTimeouts overrides the connection deadlines of the embedded
// http.Server. Equivalent to setting AppConfig.HTTPServerTimeouts. Every
// field is a *time.Duration: nil keeps the framework default
// (read-header 10s, read 60s, write 60s, idle 120s); an explicit pointer
// to 0 disables that one timeout (net/http semantics). Set a single field
// to override just that deadline without touching the others.
//
// This removes the need to re-derive the http.Server (which the framework
// owns) just to change one timeout, the same shape as WithSecurityHeaders
// for the header chain.
//
//	app := framework.NewApp(framework.WithHTTPServerTimeouts(framework.HTTPServerTimeoutsConfig{
//	    WriteTimeout: new(2 * time.Minute),
//	}))
func WithHTTPServerTimeouts(cfg HTTPServerTimeoutsConfig) AppOption {
	return func(a *App) {
		a.Config.HTTPServerTimeouts = cfg
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

// entityMountPath is the base path an entity's CRUD routes mount at:
// apiPrefix + "/" + table. With no prefix this is the historical "/table".
func (a *App) entityMountPath(table string) string {
	return a.apiPrefix() + "/" + table
}

// crudMount is where one entity's write routes live: rel on r, which
// serves them at full.
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

// entityCRUDEnabled is THE predicate for "this entity has HTTP CRUD
// routes": a DB is attached and Exposure.CRUD is unset or true. Route
// registration, collision checks, and the startup banner must all agree
// on it — the banner advertising a URL registration never mounted hands
// a first-run user a 404 (#244).
func (a *App) entityCRUDEnabled(e *entity.Entity) bool {
	return a.DB != nil && (e.Config.Exposure.CRUD == nil || *e.Config.Exposure.CRUD)
}

// EntityCRUDMounted reports whether an entity's CRUD routes were actually
// registered — the predicate route registration, the startup banner,
// openapi.json and /api/llm.md agree on, exported for surfaces that
// advertise routes from outside this package (#266). sdkdocs.Config takes
// it as CRUDMounted; Exposure.CRUD alone is not the answer, because a
// DB-less app mounts no CRUD while every entity still reads "auto". A
// read-only view mount counts as mounted: its read routes exist and are
// what such a surface documents.
//
// This wrapper was deleted once, in the #358 rework that introduced
// entityCrudMount below, while framework/docs/content/sdk.md, the
// sdkdocs.Config.CRUDMounted comment, and repolint's
// crud-exposure-rederived message all still told callers to pass it —
// the documented wiring did not compile for one release (v0.78.0).
// TestEntityCRUDMountedTracksTheMount pins the symbol and its answers.
func (a *App) EntityCRUDMounted(e *entity.Entity) bool {
	return a.entityCrudMount(e).Mounted
}

// entityCrudMount is THE predicate for the llm.md index (#358): like
// entityCRUDEnabled, but it also reports read-only mounts. App.View
// registers its entities with CrudRouteOptions{ReadOnly: true}, a fact no
// entity declaration carries — the app that performed the mount is the
// only party that knows, so the index asks it, the same way #266 threaded
// the DB-less case. A read-only view is counted and labelled as the three
// routes it serves instead of eight, five of which would 404/405.
func (a *App) entityCrudMount(e *entity.Entity) crud.MountInfo {
	if !a.entityCRUDEnabled(e) {
		// No CRUD routes, but relative custom endpoints still mount under
		// the API prefix (openapi.EntityEndpointRoutePath), so the index
		// row's base path carries it. A versioned entity keeps its Version.
		if a.apiPrefix() != "" && e.Version == "" {
			return crud.MountInfo{Path: a.entityMountPath(e.GetTable())}
		}
		return crud.MountInfo{}
	}
	// Views registered with Columns become ORM entities and mount
	// read-only (App.View); column-less views never register an entity,
	// so they never reach this predicate. Table names are unique per
	// booted app or route registration would already have panicked.
	// Path is where recordCrudMount saw the routes land, API and group
	// prefixes included: the index links there, not at /<table> (#503).
	path := a.crudMounts[e].full
	for _, v := range a.migrationViews {
		if len(v.Columns) > 0 && v.Name == e.GetTable() {
			// App.View records no mount; its routes sit at entityMountPath.
			return crud.MountInfo{Mounted: true, ReadOnly: true, Path: a.entityMountPath(e.GetTable())}
		}
	}
	return crud.MountInfo{Mounted: true, Path: path}
}

// pageIndexPath is where the app serves its page index, or "" when it
// serves none. Only a UI host built with WithPublicLLMMD registers
// /llm-pages.md (behind its mount prefix, when it has one), so the
// /api/llm.md index asks the route table rather than assuming a host.
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

// Router returns the App's *router.Router, which is where custom routes are
// registered:
//
//	a.Router().Handle("POST", "/orders/{id}/confirm", confirmOrder)
//	a.Router().Get("/healthz", healthz)
//
// Patterns are net/http ServeMux syntax: `{id}`, not `:id`. An
// Express-style `:id` segment matches literally and 404s every real
// request; `gofastr verify` catches it (GOFASTR1002).
//
// Two App-level helpers sit above it and are preferred where they apply:
//
//   - App.Use(mw):         middleware, forwards to Router().Use
//   - App.Group(prefix):   a sub-router carrying a prefix, middleware,
//     access rules, and an OpenAPI tag. Anything guarded belongs here.
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

// WithMCPTools runs register against the app's MCP server during
// InitPlugins, after every plugin and battery has registered its own
// tools and before the introspection set. It is how a package that must
// stay below the framework root (framework/docs/mcptools, which carries
// the embedded docs corpus) adds tools without importing framework:
//
//	framework.NewApp(framework.WithMCP(), framework.WithMCPTools(mcptools.Register))
//
// A registrar error fails the boot, a tool-name collision included,
// the way a plugin Init error does.
func WithMCPTools(register func(*mcp.Server) error) AppOption {
	if register == nil {
		panic("framework.WithMCPTools: nil registrar")
	}
	return func(a *App) {
		a.mcpRegistrars = append(a.mcpRegistrars, register)
	}
}

// WithMCP exposes the app's MCP server at /mcp using the Streamable HTTP
// transport (POST JSON-RPC + GET Server-Sent Events), so a host doesn't
// have to hand-wire fwApp.Router().Handle("POST", "/mcp", fwApp.MCP). This
// is the agent-ready default: combined with uihost.WithAgentReady (which
// advertises /mcp via the agent card + Link headers) it makes the server's
// tools discoverable to MCP clients. Calling this AND manually mounting
// /mcp will panic with a route conflict. Pick one.
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

// WithImagePipeline makes every schema.Image upload produce stored
// renditions plus a BlurHash (or LQIP) automatically, instead of needing a
// per-entity upload handler.
//
// Pass a deriver from framework/imagefield, which is a separate package so
// applications that do not process images never link the image decoders:
//
//	framework.WithImagePipeline(imagefield.MustNew(imagefield.Config{
//	    Variants:  []image.Variant{{Width: 960, Format: image.FormatWebP, Suffix: "md"}},
//	    BlurHashX: 4, BlurHashY: 3,
//	}))
//
// Derived values are written to sibling columns the entity declares,
// for an Image field "cover": "cover_blurhash", "cover_placeholder", and
// "cover_variants" (JSON). Columns that do not exist are skipped, so
// adopting one means adding the column and nothing else.
//
// Requires WithFileStorage; the renditions are written through the same
// backend as the original. A source that cannot be decoded fails the
// upload rather than storing a file with no renditions.
// WithStripUploadMetadata opts every auto-CRUD multipart upload into
// file.StripMetadata: EXIF, XMP and text segments are removed from the
// stored original (JPEG, PNG, WebP; other bytes pass through), with EXIF
// orientation applied first so photos still display upright. Off by
// default: originals are stored verbatim. Per-handler: CrudHandler.
// StripUploadMetadata.
func WithStripUploadMetadata() AppOption {
	return func(a *App) {
		a.stripUploadMetadata = true
	}
}

func WithImagePipeline(d file.ImageDeriver) AppOption {
	return func(a *App) {
		a.imageDeriver = d
	}
}

// WithImagePipelineFor overrides WithImagePipeline for a single image field.
// One app-wide config cannot describe every image: an avatar wants portrait
// BlurHash components and animated sources rejected, while a hero cover wants
// wide renditions. Compose both:
//
//	framework.WithImagePipeline(covers),                         // default
//	framework.WithImagePipelineFor("users", "avatar", avatars),  // override
//
// Passing a nil deriver opts that one field out of the pipeline entirely
// without unpicking the app-wide default.
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

// WithoutAutoMigrate disables the entity auto-migration that App.Start
// otherwise runs before serving. Use it in deployments whose policy
// forbids unattended schema changes on boot: generate the entity DDL
// into versioned migration files instead (`gofastr migrate generate
// <name>`) and apply them as an explicit step (`gofastr migrate up`). Entity seeds still run at Start,
// they are idempotent data, not schema, which also means an entity
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
// post-commit emit is lost, and makes delivery at-least-once per consumer.
// Consumers that care must dedupe on Event.ID. Requires WithDB; NewApp
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
// once per delivery with Event.ID set to the outbox row id (dedup key).
// It must be idempotent (at-least-once delivery). A handler that errors
// or panics is retried with backoff and eventually dead-lettered
// independently of its sibling consumers (sibling isolation).
func WithOutboxConsumer(name, eventType string, handler event.EventHandler) AppOption {
	return func(a *App) {
		a.outboxConsumers = append(a.outboxConsumers, outboxConsumerDecl{name, eventType, handler})
	}
}

// WithFanout attaches a cross-replica fanout (core/fanout.Fanout) to the
// app's real-time lane. The event bus is bridged, every locally-emitted
// event is mirrored to the other replicas and re-emitted on their buses, so
// entity `_events` SSE streams work regardless of which replica holds the
// connection, and any Mountable that supports SetFanout (a mounted UI
// host) gets its island manager wired the same way.
//
// SEMANTICS: with a fanout attached the bus becomes a broadcast. Every
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
		panic("framework: WithFanout(nil): construct a fanout first (framework/fanout.NewPostgres, core/fanout.NewRedis)")
	}
	return func(a *App) {
		a.fanout = f
	}
}

// WithGrantStore registers the app's RBAC GrantStore so the framework can
// auto-wire cross-replica grant/revoke invalidation. When a fanout is also
// attached (WithFanout), every GrantStore.Grant/Revoke publishes a
// refresh-signal on the "gofastr.access" lane and each replica re-reads
// the named role's grants from the DB into its local RolePolicy, never
// trusting the message body. Without a fanout the store is usable but
// grant/revoke stays local to this process (other replicas heal on
// restart). The store MUST already be constructed (NewGrantStore) and
// have EnsureSchema + LoadInto run on it before Start; the framework
// only wires the fanout subscription.
func WithGrantStore(gs *access.GrantStore) AppOption {
	return func(a *App) {
		a.access = gs
	}
}

// WithoutDefaultMiddleware disables the default middleware chain
// (recovery, request-id, security headers, timeout; access logging is
// NOT in the default chain — battery/log or middleware.LoggingFn add
// it). Use this when you want full control over middleware composition
// via Use().
func WithoutDefaultMiddleware() AppOption {
	return func(a *App) {
		a.noDefaults = true
	}
}

// WithLogger sets the App's *slog.Logger. Same effect as calling
// App.SetLogger after NewApp; available as an option for symmetry.
// Panics if l is nil: the App's logger is always non-nil; pass a
// discard logger (slog.New(slog.DiscardHandler)) if you want to
// silence output.
func WithLogger(l *slog.Logger) AppOption {
	if l == nil {
		panic("framework: WithLogger(nil): use slog.DiscardHandler to silence logging")
	}
	return func(a *App) {
		a.logger.Store(l)
	}
}

// WithIdempotency adds an Idempotency-Key middleware. Pass
// middleware.IdempotencyConfig{} to take all defaults; the option is
// otherwise idiomatic-Go composition over the existing
// middleware.Idempotency primitive.
//
// The middleware is mounted with router.UseInner: it runs after every
// middleware the app adds with Use (auth, tenant resolution) and next to
// the handler, so a replay is keyed on the user and tenant those
// resolved. It is independent of the default chain and works with
// WithoutDefaultMiddleware too.
//
// Defaults Principal to the request's owner (the framework's owner
// extractor, i.e. the authenticated user), falling back to a hash of the
// request's credential headers. The middleware caches nothing without a
// Principal. One shared key namespace would replay one caller's response
// body to another, and the framework layer is where "who is this" is
// knowable, so IdempotencyConfig{} still means "all defaults" here. Set
// Principal explicitly to key on something else (an API-token id).
//
// Defaults Tenant to the resolved tenant id (tenant.GetTenantID), so a
// credential that switches tenants and resends the same key runs the
// write in the new tenant.
//
// Defaults Scope to the scopes a scope-restricted API token or embed grant
// holds (access.HeldScopes), so a narrow token resending a key its user's
// session or a broader token used runs its own request instead of being
// served a response its scopes would not allow.
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
		a.idempotency = &cfg
	}
}

// requestTenantID is WithIdempotency's default Tenant: the tenant id the
// app's tenant middleware resolved, "" when there is none.
func requestTenantID(r *http.Request) string {
	return handler.GetTenantID(r.Context())
}

// heldScopeKey is WithIdempotency's default Scope: "" for a request no
// scope narrows (a session, a JWT), otherwise "scoped:" and the held
// scopes sorted, deduplicated and length-prefixed, so a token holding no
// scopes, one scope, or two in any order each get one stable namespace.
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

// ownerPrincipal keys idempotency entries by the request's owner. An
// unauthenticated request yields "anon", which shares a namespace among
// anonymous callers, acceptable because an anonymous response carries
// no per-user data by construction, and the alternative (no caching at
// all for anonymous traffic) drops the retry protection exactly where
// duplicate submits are most common.
func ownerPrincipal(r *http.Request) string {
	if id, ok := owner.Get(r.Context()); ok && id != nil {
		return fmt.Sprintf("%v", id)
	}
	// No resolved owner: the app has no auth middleware, or this route's
	// caller did not authenticate through it.
	//
	// WithIdempotency mounts the middleware innermost (router.UseInner), so
	// on that wiring the branch above sees the user the app's auth resolved.
	// A Principal used elsewhere, or a credential the app's auth does not
	// read, still lands here. Keying every such caller on one "anon"
	// namespace would let two callers sending the same Idempotency-Key,
	// "order-1" is enough, receive each other's stored response.
	//
	// The credential itself is still visible, so key on it. Different
	// credentials can never share a namespace; the cost is that
	// re-authenticating starts a new one, so a retry that straddles a login or
	// a token refresh is treated as a fresh request rather than a replay. That
	// is the right trade against serving one caller's response to another.
	if k := credentialFingerprint(r); k != "" {
		return k
	}
	return "anon"
}

// credentialFingerprint derives a stable, opaque namespace from whatever
// credential the request carries, without interpreting any of them. It is used
// only to keep distinct callers in distinct namespaces, never to authenticate.
func credentialFingerprint(r *http.Request) string {
	h := sha256.New()
	wrote := false
	for _, name := range []string{"Authorization", "X-API-Key", fembed.GrantHeader, "Cookie"} {
		// Values, not Get. Get returns only the FIRST field, and Cookie is
		// routinely sent as several: a proxy prepends its own
		// "Cookie: edge=blue" and forwards the browser's "Cookie: session_id=…"
		// as a second field. Session auth scans every cookie, so it can
		// authenticate two different users from a field this hash never saw,
		// and both of them landed in one namespace keyed on the shared first
		// field, one caller's stored response served to another, which is the
		// exact failure this function exists to prevent.
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

// WithMetrics enables HTTP request metrics (per-route counts, status classes,
// latency histograms) in the default middleware chain and mounts a
// Prometheus-format /metrics endpoint. The endpoint is unauthenticated by
// design (scrape it from inside your network / behind your ingress). Panics
// when paired with WithoutDefaultMiddleware. Mount middleware.MetricsMiddleware
// and middleware.MetricsHandler yourself in that case.
func WithMetrics() AppOption {
	return func(a *App) {
		a.metrics = middleware.NewMetrics()
	}
}

// WithDebugAuthorize decides who may read /.debug/stats and
// /.debug/goroutineleak (AppConfig.DebugEndpoints). It runs after the
// authenticated-user check, which still answers 401 for no user, and
// returns true to allow; false is a 403. Without it the caller must hold
// the "admin" role, read from the user's GetRoles() []string or from the
// roles access middleware put on the context. A request carrying an embed
// grant is refused either way, and so is a caller an access Decider denies
// on access.RoleGateCapability. A scope-restricted caller (an API token)
// must also hold access.RoleScope("admin") ("role:admin", "role:*" or
// "*:*") before fn runs: a token carries its owner's identity, and one
// minted for another surface must not read process internals because fn
// admits its owner.
func WithDebugAuthorize(fn func(ctx context.Context) bool) AppOption {
	return func(a *App) {
		a.debugAuthorize = fn
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
// Panics when paired with WithoutDefaultMiddleware. Register the
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
// A resolver of your own names the request headers it reads in its
// Vary ([i18n.LocaleResolver]), so a shared cache keys on them.
//
// Panics if used without WithI18n. Locale resolution is meaningless
// without a translator/catalog to resolve against.
func WithLocaleResolver(r i18n.LocaleResolver) AppOption {
	return func(a *App) {
		if a.translator == nil {
			panic("framework: WithLocaleResolver requires WithI18n: install a translator first")
		}
		a.localeResolver = r
	}
}

// Logger returns the App-local *slog.Logger. Middleware and plugins
// should call this, not slog.Default(), so that a logging plugin can
// replace the destination without rewiring globals.
//
// Always non-nil. NewApp seeds the App with a JSON-to-stderr logger
// that is independent of slog.Default; an unrelated slog.SetDefault
// elsewhere in the process does not redirect this App's logs.
func (a *App) Logger() *slog.Logger {
	return a.logger.Load()
}

// SetLogger replaces the App's logger. Atomic; safe to call
// concurrently with in-flight requests, atomic.Pointer.Store is
// race-free, and middleware reading via App.Logger() sees the new
// value on the next request.
//
// Panics if l is nil: the App's logger is always non-nil; pass a
// discard logger (slog.New(slog.DiscardHandler)) to silence output.
func (a *App) SetLogger(l *slog.Logger) {
	if l == nil {
		panic("framework: App.SetLogger(nil): use slog.DiscardHandler to silence logging")
	}
	a.logger.Store(l)
}

// DefaultMiddleware is the framework's standard safety chain in
// canonical order:
//
//	recovery → request-id → [i18n] → [compress] → [locale prefix] →
//	[db] → [tracing] → [metrics] → security headers → timeout
//
// The optional entries are present when the App was configured with
// WithI18n / WithLocalePrefixes / WithTracing / WithMetrics; the timeout
// entry is omitted when AppConfig.DisableRequestTimeout is true.
// WithIdempotency is not part of this chain: it mounts innermost, after
// the app's own middleware (see WithIdempotency).
//
// Access logging is deliberately NOT in this list. battery/log owns
// structured access logging when registered, and ad-hoc apps that just
// want a basic line can add middleware.LoggingFn(app.Logger) themselves.
// Having both fire produces duplicate entries with mismatched fields
// (`request` from the framework, `http.access` from the plugin).
//
// Takes the App so the recovery middleware can route panics through
// app.Logger (late-binding) and the timeout reflects
// AppConfig.RequestTimeout. Pass nil only in tests; the recovery falls
// back to slog.Default and the timeout to 30s.
func DefaultMiddleware(a *App) []router.Middleware {
	var getLogger func() *slog.Logger
	timeout := 30 * time.Second
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
	// Locale and translator go on the context ahead of every middleware
	// that can refuse a request, so an idempotency or metrics refusal
	// answers in the caller's language too (idempotency is innermost, so
	// it is inside this regardless).
	if translator != nil {
		var negOpts []i18n.NegotiateOption
		if localeResolver.Resolve != nil {
			negOpts = append(negOpts, i18n.WithLocaleResolver(localeResolver))
		}
		// A second recovery inside it answers a handler's panic in the
		// caller's locale; the outer one still catches a panicking
		// locale resolver.
		chain = append(chain, i18n.Middleware(translator, negOpts...), middleware.RecoveryFn(getLogger))
	}
	// Compress sits outside the timeout and every handler-facing
	// middleware, so it sees the final bytes and the timeout's flushes.
	if a != nil && a.Config.Compression {
		chain = append(chain, middleware.Compress())
	}
	// The locale-prefix rewriter sits inside Compress, so it reads the
	// handler's plain bytes and Compress encodes what it wrote.
	if a != nil && a.localePrefixes {
		chain = append(chain, urlprefix.Middleware())
	}
	// Stamp the App's *sql.DB onto each request context so screens and
	// handlers can reach it via DBFromContext without a package-level
	// global. No-op when the App has no DB.
	if a != nil && a.DB != nil {
		chain = append(chain, a.DBContextMiddleware())
	}
	// The App's flag evaluator, so featureflag.Bool on a request context
	// reads this App's flags and not the process default.
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

// Mount attaches a Mountable and registers its routes on the app's router
// immediately. The default middleware chain is already in place (committed
// during NewApp), so any handler the Mountable registers is wrapped with
// it. Returns the app for fluent chaining.
//
// Mountables typically register a NotFound catch-all (e.g. a UI host that
// renders pages for any unrouted path), so call Mount AFTER any explicit
// routes you want to take precedence (entity CRUD, custom endpoints).
//
// IMPORTANT. Ordering with plugins/batteries: plugin.Init runs at
// App.Start (or InitPlugins), AFTER Mount has already registered the
// Mountable's routes. If a plugin's Init registers a more-specific
// route that overlaps with a Mountable's NotFound catch-all, the
// plugin's route still wins (ServeMux dispatches by specificity, not
// by registration order). But if a plugin registers a Mountable-style
// catch-all itself, it shadows any user routes added after Mount but
// before InitPlugins. Mount last unless you know what you're doing.
func (a *App) Mount(m Mountable) *App {
	// UI hosts can expose their concrete screen paths before mounting. Check
	// those paths against already-registered entity CRUD so registration
	// order never determines whether callers get the actionable collision
	// diagnostic or an opaque ServeMux panic.
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
	// A host that renders pages learns the locale set before it serves
	// one (WithLocalePrefixes): hreflang, sitemap and the runtime's
	// locale manifest come from it. Duck-typed, like SetFanout below.
	if a.localePrefixes {
		if pl, ok := m.(interface{ SetPathLocales(i18n.PathLocales) }); ok {
			pl.SetPathLocales(a.pathLocales)
		}
	}
	m.Mount(a.router)
	// Wire the mountable into the cross-replica fanout (WithFanout) when it
	// supports it, a mounted UI host forwards to its island manager so
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
	// Hand the mounted host its session-signing key(s). Sessions are
	// stateless HMAC tokens: with a shared secret every replica verifies
	// every replica's tokens; with no secret and no fanout the host
	// self-mints a per-boot key (the single-replica zero-config path); no
	// secret WITH a fanout is a broken deployment: half of session checks
	// would 401, so it fails at boot, not in production traffic. A
	// GOFASTR_SECRET rotation drains gracefully: previous keys are handed
	// over as verify-only alongside the current one (mirroring the CSRF
	// AdditionalKeys idiom), so rotation does not log every user out at
	// once. Hosts implementing SetSessionKeys get both; older hosts fall
	// back to SetSessionKey with the current key only.
	// Resolve the session keys only when the host actually carries a
	// session-signing seam (preserves the original contract: a host with
	// no session key, e.g. a fanout-only host, is never asked about
	// secrets, so the fanout-without-secret boot panic only fires for
	// hosts that actually hold sessions).
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
	// Same seam for embeddable surfaces, with a stricter rule: an embed host
	// gets no per-boot fallback, so a host configured without an app secret
	// fails at boot. See embedKeysForMount.
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

// EmbedReserving is implemented by anything that mounts routes an embed grant
// must never reach, under a prefix the app can change.
//
// framework/embed ships a list of reserved prefixes, but it can only name
// DEFAULTS: an app that sets admin.Config.PathPrefix = "/back-office" keeps the
// protection on "/admin", which it no longer serves, and loses it on the prefix
// it does. A battery that implements this reports the prefix it ACTUALLY
// mounted, and the protection moves with it.
type EmbedReserving interface {
	ReservedEmbedPrefixes() []string
}

// BootValidator is a Mountable whose validation can only complete once
// the app's route table is complete: plugins, batteries (during
// InitPlugins), and Start itself all register routes after Mount, so a
// check that needs "everything this app will serve" cannot run there.
//
// App.Start calls ValidateBoot after the last route registration and
// before the listener binds — the latest point that can still refuse to
// serve. A panic inside ValidateBoot aborts the boot, the same contract
// as every other configuration panic.
type BootValidator interface {
	// ValidateBoot runs the mountable's deferred checks against the
	// complete route table. It may be called more than once across
	// restarts; it must not mutate the route table.
	ValidateBoot()
}

// reserveEmbedPrefixes registers every mounted battery's actual privileged
// prefix with the embed host and re-validates the declared surfaces.
//
// Surfaces are declared before anything is mounted, so a Path or Reach that
// only becomes reserved once a battery is present has to be caught here or not
// at all. A conflict is a wiring mistake with a security consequence, so it
// stops the boot rather than logging.
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

// Mountables returns the Mountables registered via Mount, in registration
// order. Batteries use it to discover a mounted UI host (type-asserting to
// *uihost.Host) so they can register screens on the host's render pipeline
// rather than spinning up a second host. Returns a copy. Callers must not
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
	// The registry keys on (name, version) so the same entity name can be
	// registered into multiple groups. The version is the group's full
	// prefix (e.g. "/api/v1"); an entity is unique per (name, prefix).
	e.Version = g.Prefix()
	// Carry the group's OpenAPI tag so the generated spec can group this
	// entity's operations under the version's tag instead of the bare name.
	e.OpenAPITag = g.OpenAPITag()

	// Read e.Config, not the raw parameter: Define normalized the grouped
	// Scope/Pagination/Exposure sub-configs into the flat fields, and the
	// grouped values are authoritative.
	crudEnabled := a.entityCRUDEnabled(e)
	if e.Config.Exposure.MCP && a.DB != nil && e.Config.Exposure.CRUD != nil && !*e.Config.Exposure.CRUD {
		panic(fmt.Sprintf("framework: entity %q has MCP=true with CRUD=false: MCP CRUD tools require the HTTP routes to be registered", name))
	}
	mcpTools := (e.Config.Exposure.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil

	// Every check that can refuse the declaration runs before the registry,
	// router or MCP server changes: App.Entity's preflight, at the group's
	// prefixed paths and MCP namespace. A refusal after Register left the
	// entity registered with half its routes.
	crudMount := ""
	if crudEnabled {
		crudMount = g.Prefix() + "/" + e.GetTable()
		// Routes() records full (prefix-applied) patterns.
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
		// The MCP tools re-dispatch through the router: that is what makes
		// them inherit auth, owner and tenant scoping rather than
		// re-implementing it, so they must address the path the routes are
		// actually mounted at. A group's sub-router shares the parent mux and
		// registers PREFIXED patterns, so dispatching to the bare "/table"
		// 404s. App.Entity sets this from the API prefix; the grouped path
		// left it empty.
		crudHandler.BasePath = g.Prefix()
		// Propagate the group's MCP namespace so tools registered for a
		// namespaced entity are disambiguated from other versions
		// (e.g. "admin.posts.list" vs "public.posts.list"). An empty
		// namespace keeps the historical flat "posts_list" tool names.
		crudHandler.MCPNamespace = g.MCPNamespace()

		// Register CRUD routes on the group's sub-router.
		// The group's prefix is already baked into the sub-router,
		// so we just mount at /<entity-table>.
		crud.RegisterCrudRoutes(g.Router(), crudHandler, "/"+e.GetTable(), crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
		a.recordCrudMount(e, g.Router(), "/"+e.GetTable(), crudMount)
	}

	// MCP tools, namespaced if the group has a namespace. Explicit
	// MCP=true, or dev-implied for CRUD-enabled entities (the dev loop
	// gives the local agent the data tools without per-entity opt-in).
	if mcpTools {
		// The group's middleware runs only on the redispatch, so the /mcp
		// context cannot show the policy the route applies.
		crudHandler.MCPRouteScoped = true
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
		path := groupEndpointPath(ent, endpoint)
		if endpoint.Handler != nil {
			// On the group's router, so the group's middleware still
			// wraps it, named by the entity's table like its CRUD routes.
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
			if err := a.MCP.RegisterTool(toolName, description, openapi.EndpointInputSchema(endpoint), endpoint.MCPHandler, endpointMCPOptions(ent, endpoint)...); err != nil {
				return err
			}
		}
	}
	return nil
}

// groupEndpointPath is where a grouped entity's endpoint mounts, relative
// to the group: EntityEndpointPath includes the version prefix for spec
// use, and the group's sub-router already carries it.
func groupEndpointPath(ent *entity.Entity, endpoint entity.Endpoint) string {
	path := "/" + strings.Trim(ent.GetTable(), "/")
	if !strings.HasPrefix(strings.TrimSpace(endpoint.Path), "/") {
		path += "/" + strings.TrimPrefix(endpoint.Path, "/")
	} else {
		path = strings.TrimSpace(endpoint.Path)
	}
	return crud.NormalizePath(convertGroupEndpointPath(path))
}

// registrationMount is where validateEntityRegistration checks an entity's
// routes and tools: App.Entity's API prefix with flat tool names, or a
// group's prefix and MCP namespace.
type registrationMount struct {
	// endpointPaths returns the full route an endpoint mounts at and the
	// path its default MCP tool name derives from.
	endpointPaths func(entity.Endpoint) (route, toolPath string)
	// mcpNamespace prefixes every tool name, as crud and
	// registerGroupEndpoints spell it; "" keeps the flat names.
	mcpNamespace string
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

// entityTool is the name crud gives the entity's action tool.
func (m registrationMount) entityTool(ent, action string) string {
	if m.mcpNamespace == "" {
		return ent + "_" + action
	}
	return m.mcpNamespace + "." + ent + "." + action
}

// endpointTool is the name an endpoint's MCP tool registers under.
func (m registrationMount) endpointTool(name string) string {
	if m.mcpNamespace == "" {
		return name
	}
	return m.mcpNamespace + "." + name
}

// convertGroupEndpointPath converts ":id"-style params to "{id}" for the
// group sub-router, mirroring openapi.convertColonParams without importing
// the unexported helper. The group sub-router already carries the prefix.
func convertGroupEndpointPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

// WrapServer adds a wrapper around the whole handler Start serves.
// Router middleware (Use) runs only for requests the router answers;
// a wrapper also covers what Start serves in the router's place: the
// interactive setup wizard, and the worker and agent roles' reduced
// muxes. It is for gates that must hold for every byte the listener
// serves, such as a Host allowlist. The first wrapper registered is
// the outermost. Call it before Start binds (a battery's Init is in
// time); a call once Start has built the server panics, since it could
// not take effect, and so does one after Shutdown.
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

// Use appends middleware to the app's router chain. The default chain
// (installed by NewApp unless WithoutDefaultMiddleware is set) stays in
// place. Use adds to it, never silently replaces it. Plugins call Use
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
// DefaultDotEnvPaths returns the dotenv files App.NewApp loads, in precedence
// order: .env.local, then .env.<APP_ENV> when APP_ENV is set, then .env.
//
// Exported so code that must read an environment variable BEFORE NewApp, a
// scaffolded main.go opening its database, for instance, can load exactly the
// same set in the same order. dotenv.Apply never overwrites an existing
// variable, so loading a shorter list first silently pins the lower-precedence
// file's value and NewApp's later load cannot correct it.
func DefaultDotEnvPaths() []string { return defaultDotEnvPaths() }

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
	// always wins. Operator-set vars are not clobbered by dotfiles.
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
		Config:        defaultAppConfig(),
		Plugins:       NewPluginManager(),
		Batteries:     NewBatteryManager(),
		events:        event.NewEventBus(),
		hooks:         make(map[string]*hook.HookRegistry),
		lc:            lifecycle.New(),
		startupOutput: os.Stdout,
	}
	// A recovered battery start or stop panic logs where the App logs.
	a.Batteries.logger = a.Logger

	for _, opt := range opts {
		opt(a)
	}
	// The gate goes on whichever server the options left in a.MCP.
	if a.mcpGate != nil && a.MCP != nil {
		a.MCP.SetGate(a.mcpGate)
	}
	// GOFASTR_SECRET is the zero-code path to an app secret (composes
	// with the dotenv load above). An explicit WithSecret wins.
	if a.secret == nil {
		if env := os.Getenv("GOFASTR_SECRET"); env != "" {
			a.secret = []byte(validateSecret(env))
		}
	}
	// GOFASTR_SECRET_PREVIOUS is the zero-code path to verify-only
	// previous secrets for graceful rotation (comma-separated; each
	// ≥32 chars), mirroring GOFASTR_SECRET. An explicit
	// WithSecretRotation option wins over the env. Each entry is
	// validated against the same length floor as the current secret.
	if !a.secretOptionSet {
		if env := os.Getenv("GOFASTR_SECRET_PREVIOUS"); env != "" {
			for _, p := range splitSecretList(env) {
				a.previousSecrets = append(a.previousSecrets, []byte(validateSecret(p)))
			}
		}
	}
	// Resolve the process role once: WithRole wins, then GOFASTR_ROLE,
	// then RoleAll. An invalid value fails loudly. A typo'd role must
	// never silently run the wrong workload (a serve process that
	// quietly starts queue workers, or vice versa).
	role, err := resolveRole(a.roleOpt, a.roleSet)
	if err != nil {
		panic(err.Error())
	}
	a.role = role

	// Seed the App-local logger if no option supplied one. JSON to
	// stderr, independent of slog.Default so external slog rewiring
	// doesn't redirect this App's framework logs. battery/log replaces
	// it during Init via app.SetLogger.
	if a.logger.Load() == nil {
		a.logger.Store(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	// Surface options WithConfig silently threw away. Filtered against the
	// FINAL config: a later granular option restoring the field means
	// nothing was ultimately lost, so it doesn't warn.
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

	// Construct the transactional outbox after all options have applied,
	// so option order between WithDB and WithOutbox doesn't matter.
	if len(a.outboxConsumers) > 0 && !a.outboxEnabled {
		// A declared consumer with no outbox would be silently dropped:
		// no durable delivery, no error. Fail loudly at construction.
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
	// attach (impossible here, one bridge per app) or a subscribe error
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

	// The default store keeps keys in this process: behind a fanout (more
	// than one replica) a retry that reaches another replica runs again.
	if a.idempotency != nil && a.idempotency.Store == nil && a.fanout != nil {
		a.Logger().Warn("framework: WithIdempotency keeps its keys in this process's memory, but WithFanout runs the app on several replicas: " +
			"a retry that reaches another replica runs the handler again; set IdempotencyConfig.Store to a shared store (middleware.NewSQLIdempotencyStore)")
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

	// Install the default middleware preset unless opted out. The router
	// resolves its middleware chain per request, so plugins can append
	// more via app.Use from their Init and still wrap routes that were
	// registered earlier (e.g. by Mount).
	if !a.noDefaults {
		a.router.Use(DefaultMiddleware(a)...)
	}
	// Idempotency runs innermost, after the app's auth and tenant
	// middleware, so the replay key carries the user and tenant they
	// resolved: a credential that switches tenants and resends the same
	// key, path and body runs the write in the new tenant.
	if a.idempotency != nil {
		a.router.UseInner(middleware.Idempotency(*a.idempotency))
	}

	// Inbound Web Bot Auth verification (experimental, draft-tracked):
	// installed whenever the option's Verify field is set, independent
	// of the default chain. The middleware runs after the defaults but
	// the router resolves the chain per request, so ordering here does
	// not weaken coverage.
	if a.webBotAuth != nil && a.webBotAuth.Verify != nil {
		a.webBotAuthVerifier = webbotauth.New(a.webBotAuth.Verify.Require, a.Logger())
		a.router.Use(a.webBotAuthVerifier.Middleware)
	}

	// Retired-markup check: in test binaries and `gofastr dev`, every
	// response whose body is markup (pages, island RPC answers, widget
	// chrome, JSON signal values) is scanned for names the upgrade
	// registry retired (internal/retired's table generated from it).
	// Installed independent of the default chain so WithoutDefaultMiddleware
	// apps are covered; in production it hands the handler its writer
	// untouched and never loads the table.
	a.router.Use(retired.Middleware(dev.Enabled))

	// Propagate DB to registry and its entities
	if a.DB != nil {
		a.Registry.SetDB(a.DB)
	}

	// Auto-wire dev-only livereload routes. No-op unless GOFASTR_DEV=1 is
	// set (typically by `gofastr dev`) and the host isn't in production.
	// See framework/dev/livereload.go for the env-gate rules.
	dev.MaybeRegisterLiveReload(a.router)
	dev.MaybeRegisterLayoutLayer(a.router)

	// Auto-enable the agent-facing MCP surface in the dev loop: /mcp
	// mount, read-only introspection, and runtime control, the dev
	// analogue of livereload, for agents instead of browsers. Same env
	// gate (GOFASTR_DEV, production GOFASTR_ENV wins), opt-out via
	// GOFASTR_DEV_MCP=0. Dev-implied flags are remembered so a host that
	// hand-mounted /mcp or registered same-named tools degrades to a
	// warning instead of the panic reserved for explicit double opt-in.
	if dev.DevMCPEnabled() {
		if !a.mcpAutoMount {
			a.mcpAutoMount, a.mcpMountDevImplied = true, true
		}
		// Dev implies the MUTATING control tools plus every entity's
		// write tools, with no auth in front of them. Pin the transport
		// to loopback so a DNS-rebound page can't reach the dispatcher:
		// after a rebind the attacker's page is same-origin, so Origin
		// alone proves nothing. Only the Host check breaks the chain.
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

// NewUIHostApp builds an App and mounts the given host on it in one call:
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
// Returns the App for fluent chaining. Panics on any misconfiguration,
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

// EntityEndpoints queues endpoints for the entity that a later App.Entity
// or TryEntity call registers under name. That call merges them into its
// config's Endpoints, so they pass the same route and MCP checks as
// endpoints written in the config. It is how a generated
// entities/endpoints.gen.go attaches endpoints to entity files it does not
// own. Calling it for an entity already registered panics, and Start fails
// while endpoints are still queued for a name nothing registered.
func (a *App) EntityEndpoints(name string, endpoints ...entity.Endpoint) *App {
	if _, err := a.Registry.Get(name); err == nil {
		panic(fmt.Sprintf("framework: EntityEndpoints(%q) after the entity was registered; queue endpoints before App.Entity", name))
	}
	if a.pendingEndpoints == nil {
		a.pendingEndpoints = map[string][]entity.Endpoint{}
	}
	a.pendingEndpoints[name] = append(a.pendingEndpoints[name], endpoints...)
	return a
}

// TryEntity is the error-returning variant of Entity: it registers an entity
// and returns an error on any misconfiguration instead of panicking. It also
// recovers panics from deeper validation (e.g. an invalid TenantField) and
// converts them to errors, so a single bad config can never take down the
// process, the property an agent-driven authoring loop needs.
//
// Registration is atomic with respect to configuration errors: every check
// that can reject the declaration, entity validation, the MCP/CRUD
// contract, route collisions, endpoint shape, runs BEFORE the registry,
// router, or MCP server is touched. A rejected declaration therefore
// leaves no registry entry, no route, and no tool, and a corrected retry
// under the same name succeeds. (The registry has no unregister and the
// router has no unmount, so validate-then-commit is the only ordering
// that can keep this promise.)
func (a *App) TryEntity(name string, config entity.EntityConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("entity %q: %v", name, textsafe.Recovered(r))
		}
	}()

	// ---- Validation phase: nothing below may mutate shared state. ----

	// SeedFS without SeedPath is a misconfiguration that would otherwise
	// silently mark the entity as seeded with empty data on first run.
	if config.SeedFS != nil && config.SeedPath == "" {
		return fmt.Errorf("entity %q has SeedFS set but SeedPath empty: point SeedPath at a file within the FS or unset SeedFS", name)
	}

	// Endpoints queued by EntityEndpoints join the config here, before
	// Define, so every check below sees them. They leave the queue only
	// once registration commits: a rejected declaration keeps them for
	// the corrected retry.
	queued := a.pendingEndpoints[name]
	if len(queued) > 0 {
		config.Endpoints = append(slices.Clip(config.Endpoints), queued...)
	}

	e := entity.Define(name, config)

	if a.DB != nil {
		e.SetDB(a.DB)
	}

	// CRUD/MCP contract.
	// Default (CRUD==nil): auto-register when DB is set.
	// Set CRUD to &true to always register, &false to opt out.
	// MCP=true implies CRUD must be mounted: MCP tools dispatch through the
	// router so they share its middleware chain (auth, recovery, etc.).
	// Read e.Config, not the raw parameter: Define normalized the grouped
	// Scope/Pagination/Exposure sub-configs into the flat fields, and the
	// grouped values are authoritative.
	crudEnabled := a.entityCRUDEnabled(e)
	if e.Config.Exposure.MCP && a.DB != nil && e.Config.Exposure.CRUD != nil && !*e.Config.Exposure.CRUD {
		return fmt.Errorf("entity %q has MCP=true with CRUD=false: MCP CRUD tools require the HTTP routes to be registered", name)
	}

	var mountPath string
	if crudEnabled {
		mountPath = a.entityMountPath(e.GetTable())
		// Pre-flight collision check: if a screen/route already owns this
		// entity's URL space, surface an actionable diagnostic that names
		// the entity, the path, and the fix. BEFORE the mux panics on the
		// opaque "/foods/llm.md conflicts with pattern" duplicate.
		if msg := a.entityRouteCollision(e, mountPath); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}

	mcpToolsEnabled := (e.Config.Exposure.MCP || (crudEnabled && dev.DevMCPEnabled())) && a.DB != nil
	if verr := a.validateEntityRegistration(e, config.Endpoints, mcpToolsEnabled, mountPath, a.plainMount(e)); verr != nil {
		return fmt.Errorf("entity %q: %w", name, verr)
	}

	// ---- Commit phase: validation passed; failures past this point are
	// framework invariant bugs, not user configuration errors. ----

	// Attribute the entity to the module whose Init is running (if any).
	if a.modules != nil {
		a.modules.recordEntity(name)
	}

	if err := a.Registry.Register(e); err != nil {
		return fmt.Errorf("failed to register entity %q: %w", name, err)
	}

	var crudHandler *crud.CrudHandler
	if crudEnabled {
		crudHandler = a.wireCrudHandler(e)
		// MCP tools dispatch in-process against a.router, where these routes are
		// mounted under the API prefix, tell the handler so its tool paths match.
		crudHandler.BasePath = a.apiPrefix()
		crud.RegisterCrudRoutes(a.router, crudHandler, mountPath, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD})
		a.recordCrudMount(e, a.router, mountPath, mountPath)
	}

	// Explicit MCP=true, or dev-implied: in the dev loop every
	// CRUD-enabled entity serves its MCP data tools so the local agent
	// can read AND write app data without per-entity opt-in. Production
	// keeps the explicit flag as the only path.
	if mcpToolsEnabled {
		if err := crud.RegisterEntityMCPTools(a.MCP, crudHandler, a.router); err != nil {
			return fmt.Errorf("failed to register MCP tools for entity %q: %w", name, err)
		}
	}

	if len(config.Endpoints) > 0 {
		if err := a.registerEntityEndpoints(e, config.Endpoints); err != nil {
			return fmt.Errorf("failed to register endpoints for entity %q: %w", name, err)
		}
	}
	delete(a.pendingEndpoints, name)

	return nil
}

// CrudHandler returns a fully-wired in-process CRUD handler for a registered
// entity, the same handler shape the HTTP routes use (hooks, events, storage,
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
	// (?status_ne= next to a `status_ne` column) is a silent wrong-column
	// filter, not an error; refuse it at the same gate.
	if err := checkFilterSuffixCollisions(ent); err != nil {
		return err
	}
	// Endpoint routes: an endpoint whose (method, path) is already taken,
	// by an existing route, by a CRUD route this same call is about to
	// mount, or by a sibling endpoint on this same declaration, would
	// panic inside router.Handle during the commit phase, i.e. AFTER the
	// registry entry and CRUD routes are already published. Recovering
	// that panic into an error cannot un-publish them, so the collision
	// has to be caught here.
	if len(endpoints) > 0 {
		// Values name what owns the route, so the error can tell a user
		// who collided with an unrelated page from one who shadowed their
		// own generated CRUD route. Different fixes.
		// Keys are normalized the same way entityRouteCollision normalizes
		// them: ServeMux conflicts on a pattern's SHAPE, so an endpoint at
		// {slug} aliases the generated {id}. Comparing raw strings let that
		// pair through validation and into the commit phase, where the
		// panic arrived with the registry entry and CRUD routes already
		// published and no way to un-publish them.
		routes := a.router.Routes()
		taken := map[string]string{}
		for _, rt := range routes {
			taken[normalizeRoutePattern(strings.ToUpper(rt.Method)+" "+rt.Pattern)] = "an existing route"
		}
		// An equal shape is not the only conflict: the scratch mux also
		// refuses an endpoint that overlaps a route with neither more
		// specific, as entityRouteCollision checks for CRUD routes.
		held := newMuxReplay(routes)
		// The CRUD routes are not on the router yet, this runs before
		// the commit phase, so ask crud for the set it will mount.
		if crudMount != "" {
			for _, pattern := range crud.CrudRoutePatterns(crudMount, crud.CrudRouteOptions{NoLLMMD: a.Config.NoLLMMD, States: ent.Config.States, Key: ent.KeyColumns()}) {
				taken[normalizeRoutePattern(pattern)] = "this entity's own generated CRUD route"
				held.add(pattern, "this entity's own generated CRUD route")
			}
		}
		for _, endpoint := range endpoints {
			method := strings.ToUpper(strings.TrimSpace(endpoint.Method))
			if method == "" || endpoint.Handler == nil {
				continue // shape errors are reported below
			}
			path, _ := m.endpointPaths(endpoint)
			key := normalizeRoutePattern(method + " " + path)
			if owner, clash := taken[key]; clash {
				return fmt.Errorf("endpoint %q would register %s, but that route is %s: rename the endpoint path, or move entity routes under an APIPrefix", endpoint.Path, key, owner)
			}
			if conflict, refused := held.add(method+" "+path, "a sibling endpoint on this declaration"); refused {
				return fmt.Errorf("endpoint %q would register %s, which overlaps %s, %s: rename the endpoint path, or move entity routes under an APIPrefix", endpoint.Path, method+" "+path, conflict.pattern, conflict.owner)
			}
			taken[key] = "a sibling endpoint on this declaration"
		}
	}

	claimed := map[string]bool{}
	if mcpTools {
		// The flat "<entity>_<action>" tool names are a documented-stable
		// public surface (see crud.RegisterEntityMCPTools), which is what
		// lets us pre-compute them here without reaching into crud.
		for _, action := range []string{"list", "get", "create", "update", "delete"} {
			claimed[m.entityTool(ent.GetName(), action)] = true
		}
		// Each non-system move is a tool named by its key, beside them.
		for _, t := range crud.RoutableTransitions(ent.Config.States) {
			claimed[m.entityTool(ent.GetName(), t.Key)] = true
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
			if err := a.MCP.RegisterTool(name, description, openapi.EndpointInputSchema(endpoint), endpoint.MCPHandler, endpointMCPOptions(ent, endpoint)...); err != nil {
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
		// tree, so they ride the dev-implied flag rather than introspection
		// itself: an explicit WithMCPIntrospection() in production must not
		// hand an MCP client a file-writing tool. See mcp_contracts_dev.go.
		if a.mcpIntrospectionDevImplied {
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
// banner: every earlier phase, auto-migrate, seeds, plugin init, OnStart
// hooks, and the bind itself, has already succeeded. Hooks run in
// registration order and must not block.
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
func (a *App) runStartHooks() error {
	a.ensureLifecycleContext()

	// Start batteries in dependency order
	if err := a.Batteries.StartAll(a.appCtx); err != nil {
		return err
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
// reverse registration order. Matches net/http.Server.Shutdown's
// signature (takes a deadline ctx) but does the FULL lifecycle teardown.
// Safe to call multiple times: the teardown runs once, and a later or
// concurrent call waits for it to finish, then returns nil, or returns
// ctx's error if its own ctx ends first.
//
// Call this from your signal handler.
func (a *App) Shutdown(ctx context.Context) error {
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
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	a.shutdownDone = done
	a.shutdownMu.Unlock()
	defer close(done)
	return a.shutdown(ctx)
}

// shutdown is Shutdown's teardown, run once.
func (a *App) shutdown(ctx context.Context) error {
	// Snapshot the cancel func and detach the server under the same lock
	// that Start/runStartHooks use to assign them. Invoke cancel OUTSIDE
	// the lock so a cancel-triggered teardown can't re-enter and deadlock.
	var firstErr error
	a.serverMu.Lock()
	cancel := a.appCancel
	a.appCancel = nil
	srv := a.server
	a.server = nil
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
	if srv != nil {
		// A connection that never sent a request would hold the drain
		// for five seconds. srv.Shutdown closes those through the
		// closeNewConns hook Start registered, once its listener is
		// closed: the stream end below can take two seconds, and a
		// connection accepted during it would be missed by a close run
		// here.
		//
		// An event stream never goes idle and a WebSocket is outside the
		// drain: end each one now rather than wait out the deadline on
		// it or leave it serving past Shutdown (shutdown_streams.go).
		a.streams.endAll(ctx)
		if err := srv.Shutdown(ctx); err != nil {
			// Bounded drain: the deadline expired with connections still
			// open (an SSE stream never goes idle, so Server.Shutdown
			// alone would leave it, and the process, hanging).
			// Force-close the stragglers so shutdown completes.
			_ = srv.Close()
			firstErr = fmt.Errorf("draining http server: %w", err)
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
// context under serverMu, the same lock that guards server. Start and
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

// Start starts the HTTP server on the given address.
func (a *App) Start(addr string) error {
	// Opt-in via GOFASTR_SEMANTIC_COVERAGE=1, so an integration test that
	// drives the real binary over HTTP still records what it exercised.
	// No-op in every other process.
	a.enableSemanticCoverageFromEnv()

	if len(a.pendingEndpoints) > 0 {
		return fmt.Errorf("EntityEndpoints queued endpoints for %s, which no App.Entity call registered", strings.Join(slices.Sorted(maps.Keys(a.pendingEndpoints)), ", "))
	}

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

	// Create the app's lifecycle context early so AutoMigrate, RunSeeds,
	// InitPlugins, and runStartHooks all share a single cancellable
	// context. A failure in any of these phases calls Shutdown, which
	// cancels the context and drains any goroutines an earlier phase
	// spawned. Without this, a startHook that spawns a worker before a
	// later startHook fails would leak that worker past Start returning.
	a.ensureLifecycleContext()

	a.warnUnresolvableRelations()
	a.guardDevMCPBind(listenAddrFor(addr))
	a.guardDevLiveReloadBind(listenAddrFor(addr))

	abort := func(err error) error {
		// Read the shutdown state BEFORE draining: the drain below calls
		// Shutdown itself, which always cancels the lifecycle context, so
		// checking afterwards could not tell an external Shutdown from
		// abort's own. appCancel nil + appCtx cancelled together mean a
		// concurrent Shutdown already ran.
		a.serverMu.Lock()
		wasShutDown := a.appCancel == nil && a.appCtx != nil && a.appCtx.Err() != nil
		a.serverMu.Unlock()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = a.Shutdown(shutdownCtx)
		// A phase failing with context.Canceled because the app's own
		// Shutdown cancelled the lifecycle context is a graceful stop,
		// not a failure: Start returns nil for a shutdown at any later
		// point, and a SIGTERM mid-migration deserves the same answer.
		if err != nil && wasShutDown && errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	if err := a.migrateAndSeed(); err != nil {
		return abort(err)
	}
	// App-level seed hooks (App.WithSeed). Run after auto-migration + the
	// per-entity RunSeeds phase so every table exists, and before plugins
	// init so a plugin that reads seed data sees it. Serialize across
	// replicas behind the SAME advisory lock as RunSeeds ( DISTINCT from
	// the migration lock) so two booting replicas don't race a hook.
	// WithSeed hooks have no ledger. They serialize-per-boot but still
	// run on every replica, so keep them idempotent. SQLite is unwrapped
	// (single-process; the lock pin would fight the pool the hooks use).
	// With no DB the call is unchanged (no coordination to do).
	if err := a.runSeedHooksSerialized(); err != nil {
		return abort(fmt.Errorf("seed hooks: %w", err))
	}

	// Initialize plugins and batteries (routes, middleware, tools, hooks).
	// Must happen after auto-migrate (so entity tables exist) but before
	// start hooks (so batteries can reference their routes).
	schemaBefore := a.schemaSize()
	if err := a.InitPlugins(); err != nil {
		return abort(fmt.Errorf("init plugins: %w", err))
	}
	// An entity, view or routine declared from a battery's or module's
	// Init (the pattern modules.md documents) missed the pass above, so
	// migrate and seed again. Both are idempotent: what the first pass
	// covered diffs to nothing and its seeds are in the ledger.
	if a.schemaSize() != schemaBefore {
		if err := a.migrateAndSeed(); err != nil {
			return abort(fmt.Errorf("after init plugins: %w", err))
		}
	}
	// `gofastr export` runs the app with GOFASTR_EXPORT set: export
	// every page now (migrations and seeds have run, so data-backed
	// pages render their rows) and return instead of serving.
	if dir := os.Getenv(envExport); dir != "" {
		if err := a.ExportStatic(a.appCtx, dir, os.Getenv(envExportBase)); err != nil {
			return abort(fmt.Errorf("export: %w", err))
		}
		a.Logger().Info("static export written", "dir", dir)
		return abort(nil)
	}
	// Check first-run setup status (WithSetup). When incomplete and not
	// disabled via GOFASTR_SETUP=off, Start either runs the steps inline
	// (headless: every required env present) or defers consumer startup
	// and serves the interactive wizard until the final step completes.
	// Worker role + incomplete setup is a hard error. Setup touches
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
					// Headless bootstrap finished. Proceed normally.
				} else {
					setupInteractive = true
				}
			}
		}
	}

	// Run OnStart hooks (cron/queue workers, custom setup). Failure here
	// aborts before we bind the port, better than a half-up server.
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
	// has fully exited: no half-delivered batch outlives the process.
	// Worker-scoped: a serve-only process stages rows (StageEvent runs in
	// the write transaction regardless of role) but never claims them.
	// Delivery belongs to the worker/all processes.
	// Deferred when interactive setup is active (started in the swap).
	if a.outbox != nil && a.runsWorkers() && !setupInteractive {
		stopRelay := a.outbox.StartRelay(a.appCtx)
		a.OnStop(func() error {
			stopRelay()
			return nil
		})
	}

	// Auto-generate and serve OpenAPI spec. Only when the app actually
	// declares entities, a UI-only app (e.g. a content site) has an
	// empty registry and gets none of these routes. The startup banner
	// below keys off the same flags so it never advertises a 404.
	hasAPI := len(a.Registry.All()) > 0
	hasLLMMD := false
	if hasAPI {
		appName := a.Config.Name
		if appName == "" {
			appName = "GoFastr API"
		}
		spec := openapi.EntityOpenAPIFor(a.Registry, openapi.Options{
			Title: appName, Version: "1.0.0",
			CRUDMounted: a.entityCRUDEnabled, BulkMounted: a.entityUIMounted,
			BasePath: a.apiPrefix(), JSONCase: a.JSONCasing(),
		})
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
			return abort(err)
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
		a.router.Get("/.well-known/mcp.json", http.HandlerFunc(a.handleMCPManifest))
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
	// owns must answer application/problem+json, not the UI host's HTML
	// 404. Installed after every mount above so the wrapped fall-through
	// is the final one. See api_miss.go.
	a.installAPIMissProblem404()

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

	name := a.Config.Name
	if name == "" {
		name = "gofastr"
	}

	// Set process title so it shows in ps / Activity Monitor
	os.Args[0] = "gofastr-" + name

	a.serverMu.Lock()
	// Resolve the real handler (full router for all/serve, health mux
	// for worker). When interactive setup is active, the server delegates
	// to whatever handlerCell currently points at, initially the setup
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
				// normal boot (which aborts Start): log and shut down.
				// The process exits, and the NEXT boot finds setup
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
	for i := len(a.serverWrappers) - 1; i >= 0; i-- {
		serveHandler = a.serverWrappers[i](serveHandler)
	}
	serveHandler = a.serveLocale(serveHandler)
	// Outermost, so a stream's context is the one every layer below sees.
	serveHandler = a.endStreamsOnShutdown(serveHandler)
	a.serverWrapped = true
	srv := &http.Server{
		Addr:              addr,
		Handler:           serveHandler,
		ConnState:         a.trackConnState,
		ReadHeaderTimeout: defaultServerReadHeaderTimeout,
		ReadTimeout:       defaultServerReadTimeout,
		WriteTimeout:      defaultServerWriteTimeout,
		IdleTimeout:       defaultServerIdleTimeout,
	}
	// net/http runs this right after Shutdown closes the listener, so it
	// also closes a connection accepted while Shutdown ended the streams.
	// The server is new on every Start, so the hook is registered once.
	srv.RegisterOnShutdown(a.closeNewConns)
	// Apply host overrides (AppConfig.HTTPServerTimeouts / WithHTTPServerTimeouts).
	// Each field is *time.Duration: nil keeps the default above; an explicit
	// pointer, including one to 0, wins. net/http treats a zero duration as
	// "no timeout", so a pointer to 0 disables that one deadline rather than
	// falling back to the default, which a plain time.Duration field could not
	// express.
	to := a.Config.HTTPServerTimeouts
	if to.ReadHeaderTimeout != nil {
		srv.ReadHeaderTimeout = *to.ReadHeaderTimeout
	}
	if to.ReadTimeout != nil {
		srv.ReadTimeout = *to.ReadTimeout
	}
	if to.WriteTimeout != nil {
		srv.WriteTimeout = *to.WriteTimeout
	}
	if to.IdleTimeout != nil {
		srv.IdleTimeout = *to.IdleTimeout
	}
	if a.Config.DisableRequestTimeout {
		// The documented opt-out for SSE and long uploads drops the Timeout
		// middleware from the chain and zeroes the server-level read/write
		// deadlines to match (header and idle stay, they bound the
		// connection, not the request body or the stream). It zeroes only the
		// fields the host left unset: an explicit HTTPServerTimeouts value is
		// authoritative, so the fine-grained knob can keep a custom deadline
		// while still dropping the middleware.
		if to.ReadTimeout == nil {
			srv.ReadTimeout = 0
		}
		if to.WriteTimeout == nil {
			srv.WriteTimeout = 0
		}
	}
	if p := a.Config.HTTPProtocols; p != nil {
		protos := *p
		srv.Protocols = &protos
	}
	tlsCfg, err := a.serverTLSConfig()
	if err != nil {
		a.serverMu.Unlock()
		return abort(err)
	}
	srv.TLSConfig = tlsCfg
	// A concurrent Shutdown may have run while the pre-listen phases
	// were still executing (a host tearing down mid-boot, or a test that
	// observed a seeded row before the server existed). It found no
	// server to stop and drained what was started; adopting a server NOW
	// would serve forever with nobody left to stop it. The record of that
	// Shutdown is appCancel cleared beside a live appCtx (the context is
	// never recreated), written under the lock checked here, so the window
	// is closed, not narrowed. The cancelled context alone is not enough:
	// Shutdown cancels it after unlocking. abort re-drains whatever started
	// after that Shutdown's sweep (Shutdown is idempotent) and nil keeps
	// the graceful-shutdown contract: Start reports no error when told to
	// stop.
	if a.appCtx != nil && (a.appCancel == nil || a.appCtx.Err() != nil) {
		a.serverMu.Unlock()
		return abort(nil)
	}
	a.server = srv
	a.serverMu.Unlock()
	// Bind first, then Serve, split from ListenAndServe so OnReady hooks
	// fire only after the port is actually held. http.ListenAndServe
	// defaults an empty Addr to ":http"; net.Listen needs that explicit.
	ln, err := net.Listen("tcp", listenAddrFor(addr))
	if err != nil {
		// Bind failure (port in use is the common case), drain like every
		// earlier start phase does, otherwise the batteries/cron/queue and
		// OnStart workers spawned above leak past Start returning.
		return abort(fmt.Errorf("listen and serve: %w", err))
	}
	// Serve closes the listener on Shutdown; this extra Close only matters
	// on the Shutdown-raced path where Serve returns without adopting ln.
	defer func() { _ = ln.Close() }()

	// Graceful shutdown by default: docker stop and kubectl rollouts send
	// SIGTERM, and without a handler the runtime kills the process
	// mid-request, no drain, no battery stop, no OnStop hooks. The
	// deferred join keeps Start from returning while a signal-triggered
	// drain is still running (Serve returns ErrServerClosed as soon as
	// Shutdown begins). Opt out via DisableSignalHandling when the host
	// process owns signals and calls Shutdown/RunWithSignals itself;
	// concurrent Shutdown calls are safe. It is idempotent.
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

	// The local run record (framework/localrun) exists while Serve
	// does: written once the port is held, removed as Start returns.
	defer a.registerLocalRun(ln.Addr().String())()

	if setupInteractive {
		setupURL = a.setup.SetupURL(a.urlScheme(), ln.Addr().String())
	}
	a.printStartupBanner(ln.Addr().String(), name, hasAPI, hasLLMMD, setupURL)
	for _, fn := range a.readyHooks {
		if rerr := a.runReadyHookSafe(fn, ln.Addr().String()); rerr != nil {
			return abort(rerr)
		}
	}
	serve := srv.Serve
	if srv.TLSConfig != nil {
		// ServeTLS adds h2 to the offered protocols and wraps ln; the
		// certificate comes from TLSConfig, so no files are named here.
		serve = func(ln net.Listener) error { return srv.ServeTLS(ln, "", "") }
	}
	if err := serve(ln); err != nil && err != http.ErrServerClosed {
		return abort(fmt.Errorf("listen and serve: %w", err))
	}
	return nil
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
func (a *App) printStartupBanner(boundAddr, name string, hasAPI, hasLLMMD bool, setupURL string) {
	base := a.urlScheme() + "://" + boundAddr
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
	fmt.Fprintf(w, "  %s Listening: %s\n", arrow(), base)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
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
