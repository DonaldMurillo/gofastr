// Package harness is the top-level composition of the GoFastr
// harness. The Harness struct wires every subsystem (engine,
// providers, tools, mcpclient, skills, context, sessions, memory,
// hooks, profile, control plane, clients) according to the v0.1
// build order in docs/harness-architecture.md.
//
// Boot sequence (per § Lifecycle / boot, 10 steps):
//
//  1. CLI parses flags, picks profile, resolves XDG paths.
//  2. Profile loads plugins.
//  3. Context reader processes context_sources (TOFU on each).
//  4. Skill registry scans paths (TOFU on each SKILL.md).
//  5. ToolSources register. MCP servers spawn with sha256 verification.
//  6. Credential helper starts.
//  7. Control plane starts (Unix socket + optional TCP).
//  8. mcpserver stdio mode reads GOFASTR_HARNESS_TOKEN if launched.
//  9. Bundled clients start (TUI / web).
//
// 10. Engine waits for first SendInput.
package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	xcontext "github.com/DonaldMurillo/gofastr/framework/harness/context"
	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/hook"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/harness/memory"
	"github.com/DonaldMurillo/gofastr/framework/harness/plugin"
	"github.com/DonaldMurillo/gofastr/framework/harness/profile"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/credstore"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/helper"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/openrouter"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/zai"
	"github.com/DonaldMurillo/gofastr/framework/harness/session"
	sqlitesession "github.com/DonaldMurillo/gofastr/framework/harness/session/sqlite"
	"github.com/DonaldMurillo/gofastr/framework/harness/skill"
	"github.com/DonaldMurillo/gofastr/framework/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/permission"
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

	// Plugins to register before boot.
	Plugins []plugin.Plugin

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

	plugins *plugin.Manager

	// sessionMu guards sessionRuns. CreateSession and Shutdown may be
	// called from different goroutines.
	sessionMu   sync.Mutex
	sessionRuns []sessionRun
}

// sessionRun bundles the per-session teardown handles created by
// CreateSession so Shutdown can release them deterministically.
type sessionRun struct {
	id     ids.SessionID
	bus    *engine.Bus
	cancel context.CancelFunc
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
		plugins: plugin.NewManager(),
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

	// (5) Tool registry — built-ins first; MCP servers spawned by
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
	// roadmap). PersistencePath enables the "Allow always" flow —
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

	// Plugins.
	for _, p := range cfg.Plugins {
		h.plugins.Register(p)
	}
	if err := h.plugins.InitAll(h); err != nil {
		return nil, fmt.Errorf("harness: init plugins: %w", err)
	}

	return h, nil
}

// CreateSession wires a fresh EngineRun bound to the given provider.
// Returns the SessionID for the caller to attach clients to.
func (h *Harness) CreateSession(prov provider.Provider, model string) ids.SessionID {
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
	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.Subscribe(ctx)
	go h.persistLoop(ctx, ch)

	// Track the teardown handles so Shutdown can cancel the persistLoop
	// context, close the bus, and unregister the engine. Without this
	// every session leaks a goroutine + context for the process lifetime.
	h.sessionMu.Lock()
	h.sessionRuns = append(h.sessionRuns, sessionRun{id: session, bus: bus, cancel: cancel})
	h.sessionMu.Unlock()

	return session
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

func (h *Harness) persistLoop(ctx context.Context, ch <-chan control.EventEnvelope) {
	for env := range ch {
		_ = h.Sessions.AppendEvent(ctx, env)
	}
}

// Shutdown releases resources held by the Harness. It tears down every
// per-session run created by CreateSession (cancelling the persistLoop
// context, closing the engine bus so subscriptions drain, and
// unregistering the engine from the mux + catalog) before closing the
// session store.
func (h *Harness) Shutdown() error {
	h.sessionMu.Lock()
	runs := h.sessionRuns
	h.sessionRuns = nil
	h.sessionMu.Unlock()

	for _, r := range runs {
		// Close the bus first so live subscriptions (incl. persistLoop's)
		// are signalled to drain, then cancel the parent context as a
		// belt-and-suspenders teardown for the persistLoop goroutine.
		if r.bus != nil {
			r.bus.Close()
		}
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

	var errs []error
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

// ---------- Plugin Host implementation ----------
//
// We implement plugin.Host with no-op stubs in v0.1; concrete wiring
// (request-middleware registration, transport registration, etc.)
// lands as the plugin system fills out. Plugins can still claim
// slash-command namespaces.

func (h *Harness) ClaimSlashCommand(namespace string) error {
	h.Catalog.ClaimNamespace(namespace)
	return nil
}

func (h *Harness) AddRequestMiddleware(_ plugin.RequestMiddleware) {}
func (h *Harness) AddToolMiddleware(_ plugin.ToolMiddleware)       {}
func (h *Harness) RegisterToolSource(src plugin.ToolSource) error {
	tsrc, ok := src.(tool.ToolSource)
	if !ok {
		return errors.New("harness: plugin ToolSource has wrong type")
	}
	return h.Tools.Register(context.Background(), tsrc)
}
func (h *Harness) RegisterProvider(p plugin.Provider) error {
	pp, ok := p.(provider.Provider)
	if !ok {
		return errors.New("harness: plugin Provider has wrong type")
	}
	h.Providers = append(h.Providers, pp)
	h.Catalog.Providers = h.Providers
	return nil
}
func (h *Harness) SubscribeEvents() <-chan plugin.EventEnvelope {
	// Cross-session subscription is v0.2 work; v0.1 returns a closed channel.
	ch := make(chan plugin.EventEnvelope)
	close(ch)
	return ch
}

// ---------- helpers ----------

func defaultSkillPaths(cfg Config) []string {
	return []string{
		filepath.Join(cfg.XDGConfig, "skills"),
		filepath.Join(cfg.WorkingDir, ".gofastr", "harness", "skills"),
	}
}

// deriveCredstoreKey picks between MachineKey (CI) and a passphrase.
// If neither is set, return an error — the caller must pick.
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
	if err := os.WriteFile(path, salt, 0o600); err != nil {
		return nil, err
	}
	return salt, nil
}

func readRandom(b []byte) (int, error) {
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Read(b)
}
