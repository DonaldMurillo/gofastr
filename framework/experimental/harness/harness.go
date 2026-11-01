// Package harness is the top-level composition of the GoFastr
// harness. The Harness struct wires the subsystems New() actually
// constructs today: the control multiplexer + resource catalog, the
// context reader, the skill registry, the built-in tool registry +
// permission engine, the credstore + in-process credential helper,
// the OpenRouter + ZAI providers, the SQLite session store, the
// file-based memory store, and the hook runner. See
// docs/harness-architecture.md for the full design (much of it
// not-yet-built) and § Lifecycle / boot for what is and is not wired.
//
// What New() does NOT do (owned by the CLI subcommand or a later
// phase): it runs no TOFU ack gate on context or skill files, their
// SHA-256 hashes are computed and stored but never compared against an
// approval store (a project skill shadowing a same-named operator
// global is disclosed with a stderr warning by skill.Registry.Load,
// not refused); it does not spawn MCP servers (the mcpclient package
// exists but is not wired into New); and it does not start the control
// plane transports or the bundled TUI/web clients, which the
// `gofastr harness` subcommand owns.
package harness

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"

	xcontext "github.com/DonaldMurillo/gofastr/framework/experimental/harness/context"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/hook"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/memory"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/profile"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/credstore"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/helper"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/openrouter"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider/zai"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
	sqlitesession "github.com/DonaldMurillo/gofastr/framework/experimental/harness/session/sqlite"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/builtins"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/permission"
)

// Config is the input to New.
type Config struct {
	// Profile is the loaded profile (framework.toml or default.toml).
	Profile *profile.Profile

	// WorkingDir is the repo root (used for AGENTS.md walk-upward).
	WorkingDir string

	// XDGConfig and XDGState are the base config + state directories
	// (`~/.config/gofastr/harness/` and `~/.local/share/gofastr/harness/`).
	XDGConfig string
	XDGState  string

	// SkillSearchPaths overrides the default paths
	// (built-in + user-global + project-local).
	SkillSearchPaths []string

	// Logger is the operational logger.
	Logger *logging.Logger

	// AllowProjectHooks gates project-local hooks (§ TOFU rule 13).
	AllowProjectHooks bool

	// CredstorePass is the passphrase used to derive the credstore key.
	// In CI, prefer GOFASTR_HARNESS_MACHINE_KEY env var instead.
	CredstorePass string

	// MachineKey overrides passphrase-derived key (CI path).
	MachineKey []byte
}

// Harness is the composed v0.1 system.
type Harness struct {
	Config Config
	Logger *logging.Logger

	Providers []provider.Provider
	Tools     *tool.Registry
	Skills    *skill.Registry
	Memory    *memory.Store
	Hooks     *hook.Runner
	Sessions  session.Store
	Creds     credstore.Store
	Helper    helper.Helper
	Perms     *permission.Engine
	Context   *xcontext.Reader

	Mux     *multiplex.Mux
	Catalog *resources.Catalog

	// sessionMu guards sessionRuns and shutdown. CreateSession and
	// Shutdown may be called from different goroutines.
	sessionMu   sync.Mutex
	sessionRuns []sessionRun
	// shutdown is set when Shutdown begins; CreateSession refuses after.
	shutdown bool
}

// ErrShutdown is CreateSession's error once Shutdown has begun.
var ErrShutdown = errors.New("harness: shut down")

// shutdownDrainTimeout bounds how long Shutdown waits for the persist
// loops to write the events already on their buses. A var so tests can
// shorten it.
var shutdownDrainTimeout = 10 * time.Second

// sessionRun bundles the per-session teardown handles created by
// CreateSession so Shutdown can release them deterministically.
type sessionRun struct {
	id     ids.SessionID
	bus    *engine.Bus
	cancel context.CancelFunc
	// persist is the session's persist loop: its exit signal and the
	// appends it could not make.
	persist *persistRun
}

// persistRun tracks one persist loop. done closes when the loop has
// written (or failed) every event its subscription delivered.
type persistRun struct {
	done     chan struct{}
	mu       sync.Mutex
	failed   int
	firstErr error
}

// fail records an append the store refused.
func (p *persistRun) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed++
	if p.firstErr == nil {
		p.firstErr = err
	}
}

// err reports the refused appends, or nil.
func (p *persistRun) err(session ids.SessionID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failed == 0 {
		return nil
	}
	return fmt.Errorf("harness: session %s: %d event(s) not persisted: %w", session, p.failed, p.firstErr)
}

// New runs the boot sequence and returns a Harness ready to drive
// sessions.
func New(cfg Config) (*Harness, error) {
	if cfg.Profile == nil {
		return nil, errors.New("harness: Profile required")
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.New(os.Stderr, logging.LevelInfo)
	}
	h := &Harness{
		Config:  cfg,
		Logger:  cfg.Logger,
		Mux:     multiplex.New(),
		Catalog: resources.NewCatalog(),
	}

	// (3) Context reader.
	h.Context = &xcontext.Reader{
		WorkingDir: cfg.WorkingDir,
		Sources:    cfg.Profile.ContextSources,
	}

	// (4) Skill registry.
	paths := cfg.SkillSearchPaths
	if len(paths) == 0 {
		paths = defaultSkillPaths(cfg)
	}
	h.Skills = skill.NewRegistry(paths...)
	if err := h.Skills.Load(); err != nil {
		h.Logger.Warn("skill load partial failure", "err", err.Error())
	}

	// (5) Tool registry, built-ins first; MCP servers spawned by
	// composition wiring after credstore + helper come up.
	h.Tools = tool.NewRegistry()
	if err := h.Tools.Register(context.Background(), builtins.Source{
		EnabledPacks: cfg.Profile.ToolPacks,
	}); err != nil {
		return nil, fmt.Errorf("harness: register built-in tools: %w", err)
	}

	// (5) Permission engine. v0.1 ships an empty profile-level rule
	// list; permissions = "preset/<name>.toml" referenced in the
	// profile is not loaded yet (separate file format on the
	// roadmap). PersistencePath enables the "Allow always" flow:
	// rules survive harness restarts via
	// $XDG_CONFIG/gofastr/harness/permissions.json.
	h.Perms = permission.New(nil)
	h.Perms.PersistencePath = filepath.Join(cfg.XDGConfig, "permissions.json")
	if err := h.Perms.LoadPersistentRules(); err != nil {
		// Non-fatal: corrupt or unreadable file means the user re-grants
		// permissions this session. Log + continue.
		if cfg.Logger != nil {
			cfg.Logger.Warn("permission persistence: load failed", "err", err)
		}
	}

	// (6) Credstore + credential helper.
	credPath := filepath.Join(cfg.XDGConfig, "creds.enc")
	key, err := deriveCredstoreKey(cfg)
	if err != nil {
		return nil, fmt.Errorf("harness: derive credstore key: %w", err)
	}
	store, err := credstore.NewEncryptedFileStore(credPath, key)
	if err != nil {
		return nil, fmt.Errorf("harness: credstore: %w", err)
	}
	h.Creds = store
	h.Helper = helper.NewInProcess(store)

	// (5) Providers. v0.1 ships openrouter + zai. Key resolution
	// order: encrypted credstore → env var fallback. Env vars are
	// populated by the secrets loader from .harness-secrets/env
	// when the CLI subcommand boots.
	or := &openrouter.Provider{}
	if apiKey, err := store.Get("openrouter", "default"); err == nil {
		or.APIKey = apiKey
	} else if envKey := os.Getenv("OPENROUTER_API_KEY"); envKey != "" {
		or.APIKey = envKey
	}
	z := &zai.Provider{}
	if apiKey, err := store.Get("zai", "default"); err == nil {
		z.APIKey = apiKey
	} else if envKey := os.Getenv("ZAI_API_KEY"); envKey != "" {
		z.APIKey = envKey
	}
	// GLM Coding Plan subscribers must use a dedicated endpoint;
	// hitting the general /api/paas/v4 endpoint returns HTTP 429
	// "Insufficient balance" (ZAI error code 1113). Set
	// ZAI_CODING_PLAN=1 in env / .harness-secrets/env to flip it.
	if v := os.Getenv("ZAI_CODING_PLAN"); v == "1" || v == "true" {
		z.CodingPlan = true
	}
	h.Providers = []provider.Provider{or, z}

	// Session store.
	sessPath := filepath.Join(cfg.XDGState, "sessions.db")
	ss, err := sqlitesession.Open(sessPath)
	if err != nil {
		return nil, fmt.Errorf("harness: session store: %w", err)
	}
	h.Sessions = ss

	// Memory store.
	memDir := filepath.Join(cfg.XDGState, "memory")
	mem, err := memory.New(memDir)
	if err != nil {
		return nil, fmt.Errorf("harness: memory: %w", err)
	}
	h.Memory = mem

	// Hook runner.
	hk := hook.New()
	hk.AllowProjectHooks = cfg.AllowProjectHooks
	h.Hooks = hk

	// Catalog wiring.
	h.Catalog.Tools = h.Tools
	h.Catalog.Providers = h.Providers
	h.Catalog.Skills = func() []skillmd.Tier1 {
		return h.Skills.Tier1Catalog()
	}

	return h, nil
}

// CreateSession wires a fresh EngineRun bound to the given provider.
// Returns the SessionID for the caller to attach clients to. Once
// Shutdown has begun it registers nothing and returns ErrShutdown: a
// session made after Shutdown took its list would never be torn down and
// would write to the store Shutdown closed.
func (h *Harness) CreateSession(prov provider.Provider, model string) (ids.SessionID, error) {
	// Held through the registration, so a session is either in the list
	// Shutdown takes or refused.
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if h.shutdown {
		return "", ErrShutdown
	}
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	disp := engine.NewDispatcher(bus, h.Tools)
	// Permission middleware wired with the multiplexer as the answer router.
	permMW := engine.PermissionMiddleware(bus, h.Perms, h.Mux, session, 0)
	disp = engine.NewDispatcher(bus, h.Tools, permMW)
	e := engine.NewEngine(session, bus, prov, model, disp)
	// Request middleware: system prompt + AGENTS.md injection.
	e.Middleware = h.buildRequestMiddleware(session)
	// Populate the engine's tool catalog from the registry so every
	// outbound Request lists the tools the model can actually call.
	// Without this, the model assumes it has no capabilities.
	e.Tools = h.toolSchemas()

	h.Mux.RegisterEngine(e)
	h.Catalog.RegisterEngine(e)

	// Subscribe the session store to the bus for persistence.
	// Subscription must happen synchronously before this function
	// returns; otherwise an early SendInput races the goroutine.
	// SubscribeAll: the log also records what sub-agents (the Agent
	// tool) run on their private buses, tagged with the sub-agent id.
	// SetLog lets a reconnecting client replay from the log what the
	// bus's in-memory window no longer holds.
	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.SubscribeAll(ctx)
	if h.Sessions != nil {
		bus.SetLog(h.Sessions)
	}
	pr := &persistRun{done: make(chan struct{})}
	go h.persistLoop(ctx, ch, pr)

	// Track the teardown handles so Shutdown can close the bus, join the
	// persistLoop, cancel its context and unregister the engine. Without
	// this every session leaks a goroutine + context for the process
	// lifetime.
	h.sessionRuns = append(h.sessionRuns, sessionRun{id: session, bus: bus, cancel: cancel, persist: pr})

	return session, nil
}

// toolSchemas converts the registered tools into provider.ToolSchema
// entries the engine attaches to each outbound Request.
func (h *Harness) toolSchemas() []provider.ToolSchema {
	if h.Tools == nil {
		return nil
	}
	tools := h.Tools.List()
	out := make([]provider.ToolSchema, 0, len(tools))
	for _, t := range tools {
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return out
}

func (h *Harness) buildRequestMiddleware(_ ids.SessionID) []engine.RequestMiddleware {
	mws := []engine.RequestMiddleware{
		engine.SystemPromptMiddleware(h.Config.Profile.PromptHeader),
	}
	if h.Context != nil {
		mws = append(mws, engine.ContextInjectionMiddleware(func(ctx context.Context) []engine.ContextSection {
			sections, err := h.Context.Read()
			if err != nil {
				return nil
			}
			var out []engine.ContextSection
			for _, s := range sections {
				out = append(out, engine.ContextSection{Name: s.Source, Content: s.Body})
			}
			return out
		}))
	}
	return mws
}

// persistLoop writes every event the subscription delivers. A closed bus
// closes the channel after the events already queued on it, so the loop
// drains them before it exits; Shutdown keeps ctx live until then.
func (h *Harness) persistLoop(ctx context.Context, ch <-chan control.EventEnvelope, pr *persistRun) {
	defer close(pr.done)
	for env := range ch {
		if err := h.appendEventGuarded(ctx, env); err != nil {
			pr.fail(err)
		}
	}
}

// appendEventGuarded isolates a panicking session store from the persist
// loop. AppendEvent is host-supplied code behind the session.Store
// interface, and the loop runs on its own goroutine with no per-request
// recover net; one panicking store implementation would take the whole
// harness process down. The event is dropped, the failure logged, and
// the panic returned as the append's error so Shutdown reports it.
func (h *Harness) appendEventGuarded(ctx context.Context, env control.EventEnvelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// textsafe.Recovered: a panic value is arbitrary
			// runtime data (C0/DEL/C1/bidi included) and this line
			// reaches the slog sink live.
			slog.Error("harness: session store AppendEvent panicked; event dropped",
				"session", env.Session, "kind", env.Kind, "panic", textsafe.Recovered(r))
			err = errors.New("session store AppendEvent panicked")
		}
	}()
	return h.Sessions.AppendEvent(ctx, env)
}

// Shutdown releases resources held by the Harness. It tears down every
// per-session run created by CreateSession: it closes the engine bus,
// waits up to shutdownDrainTimeout for the persist loop to write the
// events already queued, then cancels the loop's context and unregisters
// the engine from the mux + catalog. The session store closes last.
// Every append the store refused, and every loop still running at the
// limit, comes back in the returned error with the store's Close error.
func (h *Harness) Shutdown() error {
	h.sessionMu.Lock()
	h.shutdown = true
	runs := h.sessionRuns
	h.sessionRuns = nil
	h.sessionMu.Unlock()

	// Close every bus first: each subscription channel closes after the
	// events queued on it, so the persist loops drain and exit. Their
	// contexts stay live meanwhile, or the queued appends fail on a
	// cancelled context.
	for _, r := range runs {
		if r.bus != nil {
			r.bus.Close()
		}
	}
	var errs []error
	limit := time.NewTimer(shutdownDrainTimeout)
	defer limit.Stop()
	expired := false
	for _, r := range runs {
		if r.persist == nil {
			continue
		}
		if !expired {
			select {
			case <-r.persist.done:
			case <-limit.C:
				expired = true
			}
		}
		if expired {
			select {
			case <-r.persist.done:
			default:
				errs = append(errs, fmt.Errorf("harness: session %s: persist loop still writing after %s; later events not persisted", r.id, shutdownDrainTimeout))
			}
		}
		if err := r.persist.err(r.id); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range runs {
		if r.cancel != nil {
			r.cancel()
		}
		if h.Mux != nil {
			h.Mux.UnregisterEngine(r.id)
		}
		if h.Catalog != nil {
			h.Catalog.UnregisterEngine(r.id)
		}
	}

	if h.Sessions != nil {
		if err := h.Sessions.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return nil
}

// ---------- helpers ----------

func defaultSkillPaths(cfg Config) []string {
	return []string{
		filepath.Join(cfg.XDGConfig, "skills"),
		filepath.Join(cfg.WorkingDir, ".gofastr", "harness", "skills"),
	}
}

// deriveCredstoreKey picks between MachineKey (CI) and a passphrase.
// If neither is set, return an error, the caller must pick.
func deriveCredstoreKey(cfg Config) ([]byte, error) {
	if len(cfg.MachineKey) == 32 {
		return cfg.MachineKey, nil
	}
	if cfg.CredstorePass != "" {
		// Salt: a stable per-XDGConfig file.
		saltPath := filepath.Join(cfg.XDGConfig, "salt")
		if err := os.MkdirAll(filepath.Dir(saltPath), 0o700); err != nil {
			return nil, err
		}
		salt, err := readOrCreateSalt(saltPath)
		if err != nil {
			return nil, err
		}
		return credstore.DeriveKey([]byte(cfg.CredstorePass), salt), nil
	}
	return nil, errors.New("harness: need CredstorePass or MachineKey")
}

func readOrCreateSalt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 16 {
		return data, nil
	}
	if !os.IsNotExist(err) && err != nil {
		return nil, err
	}
	salt := make([]byte, 32)
	if _, err := readRandom(salt); err != nil {
		return nil, err
	}
	// WriteOwnerOnly: the salt derives the credstore key, and the mode
	// must tighten a pre-existing weaker file, not just apply at
	// create.
	if err := fileperm.WriteOwnerOnly(path, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func readRandom(b []byte) (int, error) {
	return rand.Read(b)
}
