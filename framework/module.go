package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ModuleManifest is the declarative metadata a Module carries alongside
// its Battery Init. Everything in it is informational — the framework
// uses DependsOn for battery init ordering (it doubles as the battery
// dep list), and the rest is surfaced through introspection.
type ModuleManifest struct {
	// Version is an optional, informational version string.
	Version string

	// Description is a short, human-readable summary.
	Description string

	// DependsOn names other MODULES that must be enabled (and initialised)
	// before this one. RegisterModule forwards this to BatteryManager as
	// the battery dep list, so topo-sort orders module init.
	DependsOn []string

	// MigrationGroup defaults to the module name. It is an informational
	// pointer to the core/migrate group (#33) the module owns — the
	// framework does not enforce that it matches a registered migration
	// group, but the modules doc describes the tie-in.
	MigrationGroup string
}

// Module is a Battery plus a manifest. Everything a module registers
// during Init (routes, entities, cron jobs, queue consumers, MCP tools)
// is attributed to the module, and a runtime enable/disable gate is
// enforced at dispatch time: disabled → its routes 404, its cron jobs
// and queue consumers skip, its MCP tools refuse.
type Module interface {
	Battery
	Manifest() ModuleManifest
}

// ModuleInfo is the introspection view of a registered module.
type ModuleInfo struct {
	Name           string
	Version        string
	Description    string
	DependsOn      []string
	MigrationGroup string
	Enabled        bool
	EntityCount    int
	RouteCount     int
	ToolCount      int
}

// ModuleStore persists module enable/disable state across restarts.
// The default in-memory store is used when the app has no DB; an app
// built with WithDB gets the SQL store automatically.
type ModuleStore interface {
	Load(ctx context.Context) (map[string]bool, error)
	SetEnabled(ctx context.Context, name string, enabled bool) error
}

// ---------------------------------------------------------------------------
// In-memory store
// ---------------------------------------------------------------------------

// InMemoryModuleStore is the default store for apps without a DB. State
// is lost on restart — modules re-enable on every boot.
type InMemoryModuleStore struct {
	mu   sync.Mutex
	data map[string]bool
}

// NewInMemoryModuleStore creates an empty in-memory store.
func NewInMemoryModuleStore() *InMemoryModuleStore {
	return &InMemoryModuleStore{data: make(map[string]bool)}
}

// Load returns the current enable/disable map (possibly empty).
func (s *InMemoryModuleStore) Load(_ context.Context) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

// SetEnabled persists the enabled state in memory.
func (s *InMemoryModuleStore) SetEnabled(_ context.Context, name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[name] = enabled
	return nil
}

// ---------------------------------------------------------------------------
// SQL store
// ---------------------------------------------------------------------------

// SQLModuleStore persists module state in a gofastr_modules table.
// Self-migrating (CREATE TABLE IF NOT EXISTS) — not a migrate group.
type SQLModuleStore struct {
	db      *sql.DB
	dialect migrate.Dialect
	now     func() time.Time
}

// NewSQLModuleStore creates a SQL-backed store and ensures the table
// exists. dialect is probed via migrate.DetectDialect when not explicit.
func NewSQLModuleStore(db *sql.DB) (*SQLModuleStore, error) {
	s := &SQLModuleStore{
		db:      db,
		dialect: migrate.DetectDialect(db),
		now:     time.Now,
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("module store: ensure table: %w", err)
	}
	return s, nil
}

func (s *SQLModuleStore) ensureTable() error {
	stmt := `CREATE TABLE IF NOT EXISTS gofastr_modules (
		name       TEXT PRIMARY KEY,
		enabled    BOOLEAN NOT NULL,
		updated_at TIMESTAMP NOT NULL
	)`
	_, err := s.db.Exec(stmt)
	return err
}

// Load returns every persisted enable/disable row.
func (s *SQLModuleStore) Load(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, enabled FROM gofastr_modules")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		out[name] = enabled
	}
	return out, rows.Err()
}

// SetEnabled upserts a module's enabled state.
func (s *SQLModuleStore) SetEnabled(ctx context.Context, name string, enabled bool) error {
	const stmt = `INSERT INTO gofastr_modules (name, enabled, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET enabled = $2, updated_at = CURRENT_TIMESTAMP`
	_, err := s.db.ExecContext(ctx, stmt, name, enabled)
	return err
}

// ---------------------------------------------------------------------------
// Module manager
// ---------------------------------------------------------------------------

// moduleEntry holds a registered module's manifest.
type moduleEntry struct {
	manifest ModuleManifest
}

// ModuleManager tracks registered modules, their enable/disable state,
// and the attribution of routes/tools/entities to modules. It is the
// runtime gate that disabled-module enforcement reads on every request.
type ModuleManager struct {
	mu      sync.RWMutex
	modules map[string]*moduleEntry
	order   []string // registration order

	// enabled is the in-memory cache loaded from the store at boot.
	// Modules absent from the map are treated as enabled (default).
	enabled map[string]bool

	// Attribution maps, populated during module Init.
	routes   map[string]string // "METHOD /path" → module name
	tools    map[string]string // tool name → module name
	entities map[string]string // entity name → module name

	// current is the module whose Init is running (attribution target).
	// Set/cleared under mu by the battery init loop. Boot is
	// single-threaded through BatteryManager.InitAll.
	current string

	// toggleMu serializes Enable/Disable check-then-act sequences
	// (dependency check → store write → cache flip) so concurrent
	// toggles cannot interleave to a forbidden state. Separate from mu
	// (the read-path RWMutex) so Enabled() stays cheap.
	toggleMu sync.Mutex

	store    ModuleStore
	storeErr error         // non-nil if SQL store creation failed with a DB
	fanout   fanout.Fanout // nil = single-replica
	nodeID   string        // for fanout self-dedup
	db       *sql.DB       // nil when no DB
}

// NewModuleManager creates a manager backed by the appropriate store.
// When db is non-nil a SQLModuleStore is used; otherwise in-memory.
// If db is non-nil and the SQL store cannot be created (e.g. CREATE TABLE
// fails), the error is stored and surfaced by loadFromStore so boot fails
// closed — a deliberately disabled module must not silently come back
// enabled on a broken store.
func NewModuleManager(db *sql.DB, f fanout.Fanout) *ModuleManager {
	mm := &ModuleManager{
		modules:  make(map[string]*moduleEntry),
		enabled:  make(map[string]bool),
		routes:   make(map[string]string),
		tools:    make(map[string]string),
		entities: make(map[string]string),
		fanout:   f,
		nodeID:   fanout.NewNodeID(),
		db:       db,
	}
	if db != nil {
		store, err := NewSQLModuleStore(db)
		if err == nil {
			mm.store = store
		} else {
			// Store creation failed with a DB provided — propagate the
			// error via storeErr so loadFromStore fails the boot.
			// Do NOT fall back to in-memory: that would silently
			// re-enable every deliberately-disabled module.
			mm.storeErr = fmt.Errorf("create modules table: %w", err)
		}
	} else {
		mm.store = NewInMemoryModuleStore()
	}
	return mm
}

// register records a module's manifest. Called by App.RegisterModule.
func (mm *ModuleManager) register(name string, manifest ModuleManifest) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.modules[name] = &moduleEntry{manifest: manifest}
	mm.order = append(mm.order, name)
}

// setCurrent marks the module whose Init is about to run so that
// registration hooks attribute routes/tools/entities to it.
func (mm *ModuleManager) setCurrent(name string) {
	mm.mu.Lock()
	mm.current = name
	mm.mu.Unlock()
}

// clearCurrent removes the current-module marker. Called after each
// module's Init returns (or when a non-module battery inits).
func (mm *ModuleManager) clearCurrent() {
	mm.mu.Lock()
	mm.current = ""
	mm.mu.Unlock()
}

// currentModule returns the module currently being initialised, or "".
func (mm *ModuleManager) currentModule() string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.current
}

// recordRoute stamps a route with the current module. Called by
// the router register hook. The key is method + " " + pattern so two
// modules owning different methods on the same path are attributed
// independently.
func (mm *ModuleManager) recordRoute(method, pattern string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.routes[method+" "+pattern] = mm.current
	}
	mm.mu.Unlock()
}

// recordTool stamps an MCP tool name with the current module.
func (mm *ModuleManager) recordTool(toolName string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.tools[toolName] = mm.current
	}
	mm.mu.Unlock()
}

// recordEntity stamps an entity name with the current module.
func (mm *ModuleManager) recordEntity(entityName string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.entities[entityName] = mm.current
	}
	mm.mu.Unlock()
}

// loadFromStore populates the enabled cache from the persisted store.
// Called once at boot, after batteries init. Modules absent from the
// store are enabled by default; store rows for unknown modules are
// kept but never read.
func (mm *ModuleManager) loadFromStore(ctx context.Context) error {
	if mm.storeErr != nil {
		return mm.storeErr
	}
	state, err := mm.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("module store load: %w", err)
	}
	mm.mu.Lock()
	defer mm.mu.Unlock()
	for name := range mm.modules {
		if v, ok := state[name]; ok {
			mm.enabled[name] = v
		} else {
			mm.enabled[name] = true
		}
	}
	return nil
}

// Enabled is the hot-path read the dispatch gates use. A single map
// read under RLock; absent modules are enabled by default.
func (mm *ModuleManager) Enabled(name string) bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	v, ok := mm.enabled[name]
	if !ok {
		return true
	}
	return v
}

// routeAllowed returns false when the route's owning module is disabled.
// The key is "METHOD /path" (matching recordRoute). Routes with no owner
// pass.
func (mm *ModuleManager) routeAllowed(key string) bool {
	mm.mu.RLock()
	owner := mm.routes[key]
	mm.mu.RUnlock()
	if owner == "" {
		return true
	}
	return mm.Enabled(owner)
}

// toolAllowed returns an error when the tool's owning module is disabled.
// Tools with no owner pass.
func (mm *ModuleManager) toolAllowed(toolName string) error {
	mm.mu.RLock()
	owner := mm.tools[toolName]
	mm.mu.RUnlock()
	if owner == "" {
		return nil
	}
	if !mm.Enabled(owner) {
		return fmt.Errorf("module %q is disabled", owner)
	}
	return nil
}

// cronGate returns a gate func that skips jobs whose owning module is
// disabled. nil currentModule means no module owns the scheduler.
func (mm *ModuleManager) cronGate(moduleName string) func(string) bool {
	return func(_ string) bool {
		return mm.Enabled(moduleName)
	}
}

// queueGate returns a gate func that defers jobs whose owning module
// is disabled.
func (mm *ModuleManager) queueGate(moduleName string) func(string) bool {
	return func(_ string) bool {
		return mm.Enabled(moduleName)
	}
}

// hasModule reports whether name is a registered module.
func (mm *ModuleManager) hasModule(name string) bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	_, ok := mm.modules[name]
	return ok
}

// dependents returns the names of currently-enabled modules that list
// name in their DependsOn.
func (mm *ModuleManager) dependents(name string) []string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	var deps []string
	for _, modName := range mm.order {
		entry := mm.modules[modName]
		if entry == nil {
			continue
		}
		for _, d := range entry.manifest.DependsOn {
			if d == name {
				if mm.enabled[modName] { // only block if the dependent is enabled
					deps = append(deps, modName)
				}
				break
			}
		}
	}
	return deps
}

// Enable persists the new state and flips the cache. Refuses if any of
// the module's DependsOn is disabled. The check-then-act sequence
// (dependency check → store write → cache flip) is serialized by
// toggleMu so concurrent toggles cannot interleave to a forbidden state.
func (mm *ModuleManager) Enable(ctx context.Context, name string) error {
	mm.toggleMu.Lock()
	defer mm.toggleMu.Unlock()

	if !mm.hasModule(name) {
		return fmt.Errorf("module %q is not registered", name)
	}
	mm.mu.RLock()
	entry := mm.modules[name]
	mm.mu.RUnlock()
	for _, dep := range entry.manifest.DependsOn {
		if !mm.hasModule(dep) {
			return fmt.Errorf("module %q depends on unregistered module %q", name, dep)
		}
		if !mm.Enabled(dep) {
			return fmt.Errorf("cannot enable module %q: dependency %q is disabled", name, dep)
		}
	}
	if err := mm.store.SetEnabled(ctx, name, true); err != nil {
		return fmt.Errorf("persist enable for %q: %w", name, err)
	}
	mm.mu.Lock()
	mm.enabled[name] = true
	mm.mu.Unlock()
	mm.publish(ctx, name, true)
	return nil
}

// Disable persists the new state and flips the cache. Refuses (fail
// closed) if any currently-enabled module lists name in DependsOn — no
// cascade. Serialized by toggleMu alongside Enable.
func (mm *ModuleManager) Disable(ctx context.Context, name string) error {
	mm.toggleMu.Lock()
	defer mm.toggleMu.Unlock()

	if !mm.hasModule(name) {
		return fmt.Errorf("module %q is not registered", name)
	}
	deps := mm.dependents(name)
	if len(deps) > 0 {
		return fmt.Errorf("cannot disable module %q: enabled modules %v depend on it", name, deps)
	}
	if err := mm.store.SetEnabled(ctx, name, false); err != nil {
		return fmt.Errorf("persist disable for %q: %w", name, err)
	}
	mm.mu.Lock()
	mm.enabled[name] = false
	mm.mu.Unlock()
	mm.publish(ctx, name, false)
	return nil
}

// moduleToggleMessage is the fanout payload for a remote enable/disable.
type moduleToggleMessage struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// publish broadcasts a toggle to other replicas via fanout. No-op when
// no fanout is attached. A Publish failure is logged at Warn so a dead
// bus doesn't silently strand stale replicas.
func (mm *ModuleManager) publish(_ context.Context, name string, enabled bool) {
	if mm.fanout == nil {
		return
	}
	payload, _ := json.Marshal(moduleToggleMessage{Name: name, Enabled: enabled})
	envelope := fanout.Wrap(mm.nodeID, payload)
	if err := mm.fanout.Publish(context.Background(), "gofastr.modules", envelope); err != nil {
		log.Printf("WARN: module manager: fanout publish for %q failed: %v — other replicas may be stale until restart", name, err)
	}
}

// handleRemoteToggle processes a fanout message from another replica.
// The message is treated as a REFRESH SIGNAL only: the payload's
// Enabled field is never trusted directly. Instead the authoritative
// state is re-read from the store and the cache is set to whatever the
// store says. This makes message ordering irrelevant (the store is the
// source of truth) and neuters crafted payloads (they can only trigger
// a re-read). Unknown module names are ignored so they cannot pollute
// the cache.
func (mm *ModuleManager) handleRemoteToggle(raw []byte) {
	nodeID, body, err := fanout.Unwrap(raw)
	if err != nil {
		return
	}
	if nodeID == mm.nodeID {
		return // own publish
	}
	var msg moduleToggleMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return
	}
	// Ignore names not registered as modules — a crafted or stale
	// payload must not pollute the cache.
	if !mm.hasModule(msg.Name) {
		return
	}
	// Re-read authoritative state from the store.
	state, err := mm.store.Load(context.Background())
	if err != nil {
		log.Printf("WARN: module manager: store load on fanout refresh for %q failed: %v — keeping current cache", msg.Name, err)
		return
	}
	mm.mu.Lock()
	if v, ok := state[msg.Name]; ok {
		mm.enabled[msg.Name] = v
	}
	mm.mu.Unlock()
}

// subscribeFanout registers the manager's fanout listener. Called once
// at boot when a fanout is attached.
func (mm *ModuleManager) subscribeFanout() error {
	if mm.fanout == nil {
		return nil
	}
	_, err := mm.fanout.Subscribe("gofastr.modules", func(payload []byte) {
		mm.handleRemoteToggle(payload)
	})
	return err
}

// List returns introspection info for every registered module.
func (mm *ModuleManager) List() []ModuleInfo {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	out := make([]ModuleInfo, 0, len(mm.order))
	for _, name := range mm.order {
		entry := mm.modules[name]
		if entry == nil {
			continue
		}
		m := entry.manifest
		mg := m.MigrationGroup
		if mg == "" {
			mg = name
		}
		// Count owned surfaces.
		var routes, tools, entities int
		for _, mod := range mm.routes {
			if mod == name {
				routes++
			}
		}
		for _, mod := range mm.tools {
			if mod == name {
				tools++
			}
		}
		for _, mod := range mm.entities {
			if mod == name {
				entities++
			}
		}
		enabled := mm.enabled[name]
		if _, ok := mm.enabled[name]; !ok {
			enabled = true
		}
		out = append(out, ModuleInfo{
			Name:           name,
			Version:        m.Version,
			Description:    m.Description,
			DependsOn:      append([]string{}, m.DependsOn...),
			MigrationGroup: mg,
			Enabled:        enabled,
			EntityCount:    entities,
			RouteCount:     routes,
			ToolCount:      tools,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// App integration
// ---------------------------------------------------------------------------

// RegisterModule validates the name, registers the module as a battery
// (with deps = Manifest().DependsOn so topo-sort orders module init),
// and records the manifest in the module manager.
//
//	app.RegisterModule(myModule) // registers as battery + records manifest
func (a *App) RegisterModule(m Module) *App {
	if a.initialized.Load() {
		panic(fmt.Sprintf("framework: RegisterModule(%q) called after InitPlugins; register modules before App.Start", m.Name()))
	}
	manifest := m.Manifest()
	if err := a.Batteries.Register(m, manifest.DependsOn...); err != nil {
		panic(fmt.Sprintf("framework: failed to register module %q: %v", m.Name(), err))
	}
	a.modules.register(m.Name(), manifest)
	return a
}

// Modules returns the app's module manager, through which callers can
// list modules, query enabled state, and toggle modules at runtime.
func (a *App) Modules() *ModuleManager {
	return a.modules
}
