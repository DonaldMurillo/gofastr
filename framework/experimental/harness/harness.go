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
	// Profile is required.
	Profile *profile.Profile
	// WorkingDir is where project context and project skills are read from.
	WorkingDir string
	// XDGConfig holds credentials, the salt, permissions.json and user skills; XDGState holds sessions.db and memory.
	XDGConfig string
	XDGState  string
	// SkillSearchPaths replaces the default user and project skill directories when set.
	SkillSearchPaths []string
	// Logger defaults to info level on stderr.
	Logger *logging.Logger
	// AllowProjectHooks lets hooks with Source "project" register.
	AllowProjectHooks bool
	// CredstorePass derives the credstore key when MachineKey is not a 32-byte key.
	CredstorePass string
	// MachineKey is used as the credstore key as-is when it is 32 bytes.
	MachineKey []byte
}

// Harness holds the subsystems New wires together.
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

	sessionMu   sync.Mutex
	sessionRuns []sessionRun
	shutdown    bool
}

// ErrShutdown is CreateSession's error once Shutdown has begun.
var ErrShutdown = errors.New("harness: shut down")

var shutdownDrainTimeout = 10 * time.Second

type sessionRun struct {
	id      ids.SessionID
	bus     *engine.Bus
	cancel  context.CancelFunc
	persist *persistRun
}

type persistRun struct {
	done     chan struct{}
	mu       sync.Mutex
	failed   int
	firstErr error
}

func (p *persistRun) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed++
	if p.firstErr == nil {
		p.firstErr = err
	}
}

func (p *persistRun) err(session ids.SessionID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failed == 0 {
		return nil
	}
	return fmt.Errorf("harness: session %s: %d event(s) not persisted: %w", session, p.failed, p.firstErr)
}

// New builds every subsystem from cfg and returns a Harness ready for CreateSession; it errors when no Profile is given or no credstore key can be derived.
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

	h.Context = &xcontext.Reader{
		WorkingDir: cfg.WorkingDir,
		Sources:    cfg.Profile.ContextSources,
	}

	paths := cfg.SkillSearchPaths
	if len(paths) == 0 {
		paths = defaultSkillPaths(cfg)
	}
	h.Skills = skill.NewRegistry(paths...)
	if err := h.Skills.Load(); err != nil {
		h.Logger.Warn("skill load partial failure", "err", err.Error())
	}

	h.Tools = tool.NewRegistry()
	if err := h.Tools.Register(context.Background(), builtins.Source{
		EnabledPacks: cfg.Profile.ToolPacks,
	}); err != nil {
		return nil, fmt.Errorf("harness: register built-in tools: %w", err)
	}

	h.Perms = permission.New(nil)
	h.Perms.PersistencePath = filepath.Join(cfg.XDGConfig, "permissions.json")
	if wd, err := os.Getwd(); err == nil {
		h.Perms.WorkingDir = wd
	}
	if err := h.Perms.LoadPersistentRules(); err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("permission persistence: load failed", "err", err)
		}
	}

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
	if v := os.Getenv("ZAI_CODING_PLAN"); v == "1" || v == "true" {
		z.CodingPlan = true
	}
	h.Providers = []provider.Provider{or, z}

	sessPath := filepath.Join(cfg.XDGState, "sessions.db")
	ss, err := sqlitesession.Open(sessPath)
	if err != nil {
		return nil, fmt.Errorf("harness: session store: %w", err)
	}
	h.Sessions = ss

	memDir := filepath.Join(cfg.XDGState, "memory")
	mem, err := memory.New(memDir)
	if err != nil {
		return nil, fmt.Errorf("harness: memory: %w", err)
	}
	h.Memory = mem

	hk := hook.New()
	hk.AllowProjectHooks = cfg.AllowProjectHooks
	h.Hooks = hk

	h.Catalog.Tools = h.Tools
	h.Catalog.Providers = h.Providers
	h.Catalog.Skills = func() []skillmd.Tier1 {
		return h.Skills.Tier1Catalog()
	}

	return h, nil
}

// CreateSession starts an engine for prov and model whose events are persisted, and returns its SessionID; after Shutdown has begun it returns ErrShutdown.
func (h *Harness) CreateSession(prov provider.Provider, model string) (ids.SessionID, error) {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if h.shutdown {
		return "", ErrShutdown
	}
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	permMW := engine.PermissionMiddleware(bus, h.Perms, h.Mux, session, 0)
	disp := engine.NewDispatcher(bus, h.Tools, permMW)
	e := engine.NewEngine(session, bus, prov, model, disp)
	e.Middleware = h.buildRequestMiddleware(session)
	e.Tools = h.toolSchemas()

	h.Mux.RegisterEngine(e)
	h.Catalog.RegisterEngine(e)

	ctx, cancel := context.WithCancel(context.Background())
	ch := bus.SubscribeAll(ctx)
	if h.Sessions != nil {
		bus.SetLog(h.Sessions)
	}
	pr := &persistRun{done: make(chan struct{})}
	go h.persistLoop(ctx, ch, pr)

	h.sessionRuns = append(h.sessionRuns, sessionRun{id: session, bus: bus, cancel: cancel, persist: pr})

	return session, nil
}

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

func (h *Harness) persistLoop(ctx context.Context, ch <-chan control.EventEnvelope, pr *persistRun) {
	defer close(pr.done)
	for env := range ch {
		if err := h.appendEventGuarded(ctx, env); err != nil {
			pr.fail(err)
		}
	}
}

func (h *Harness) appendEventGuarded(ctx context.Context, env control.EventEnvelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("harness: session store AppendEvent panicked; event dropped",
				"session", env.Session, "kind", env.Kind, "panic", textsafe.Recovered(r))
			err = errors.New("session store AppendEvent panicked")
		}
	}()
	return h.Sessions.AppendEvent(ctx, env)
}

// Shutdown closes every session, waits a bounded time for queued events to persist, closes the session store, and returns every append and close error.
func (h *Harness) Shutdown() error {
	h.sessionMu.Lock()
	h.shutdown = true
	runs := h.sessionRuns
	h.sessionRuns = nil
	h.sessionMu.Unlock()

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

func defaultSkillPaths(cfg Config) []string {
	return []string{
		filepath.Join(cfg.XDGConfig, "skills"),
		filepath.Join(cfg.WorkingDir, ".gofastr", "harness", "skills"),
	}
}

func deriveCredstoreKey(cfg Config) ([]byte, error) {
	if len(cfg.MachineKey) == 32 {
		return cfg.MachineKey, nil
	}
	if cfg.CredstorePass != "" {
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
	if err := fileperm.WriteOwnerOnly(path, salt); err != nil {
		return nil, err
	}
	return salt, nil
}

func readRandom(b []byte) (int, error) {
	return rand.Read(b)
}
