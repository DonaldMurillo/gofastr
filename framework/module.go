package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ModuleManifest is a Module's metadata. DependsOn is also its battery
// dependency list; the rest is informational.
type ModuleManifest struct {
	Version     string
	Description string
	// DependsOn names modules that must be enabled, and initialized, first.
	DependsOn []string
	// MigrationGroup defaults to the module name; it is not checked.
	MigrationGroup string
}

// Module is a Battery plus a manifest. What it registers during Init is
// attributed to it, and while it is disabled its routes 404, its cron
// jobs and queue consumers skip and its MCP tools refuse.
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

	// The process fields below are zero for an in-process module.
	ProcessState string
	// RestartCount counts unexpected exits in the current circuit window.
	RestartCount       int
	ObservedGeneration uint64
	InstanceID         string
	// LastExit is "", "drained", "crashed: <err>", "lease-expired-drain",
	// "failed: <reason>" or "exited-cleanly-unexpected".
	LastExit string
}

// ModuleStore persists module enable/disable state. An app with a DB
// uses SQLModuleStore, one without uses InMemoryModuleStore.
type ModuleStore interface {
	Load(ctx context.Context) (map[string]bool, error)
	SetEnabled(ctx context.Context, name string, enabled bool) error
}

// InMemoryModuleStore holds module state for the life of the process.
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
	maps.Copy(out, s.data)
	return out, nil
}

// SetEnabled persists the enabled state in memory.
func (s *InMemoryModuleStore) SetEnabled(_ context.Context, name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[name] = enabled
	return nil
}

// SQLModuleStore persists module state in the gofastr_modules table.
type SQLModuleStore struct {
	db      *sql.DB
	dialect migrate.Dialect
	now     func() time.Time
}

// NewSQLModuleStore creates the store, creating its table if missing.
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
		name       TEXT PRIMARY KEY NOT NULL,
		enabled    BOOLEAN NOT NULL,
		updated_at TIMESTAMP NOT NULL
	)`
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	registerModuleExport()
	return nil
}

func registerModuleExport() {
	datexport.Register(datexport.DataExporter{
		Name: "gofastr_modules", Source: "modules", Table: "gofastr_modules",
		PrimaryKey: "name", Columns: []string{"name", "enabled", "updated_at"},
	})
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

type moduleEntry struct {
	manifest ModuleManifest
}

// ModuleManager tracks registered modules, their enabled state, and which
// module owns each route, tool and entity; the dispatch gates read it.
type ModuleManager struct {
	mu      sync.RWMutex
	modules map[string]*moduleEntry
	order   []string
	enabled map[string]bool

	routes   map[string]string
	tools    map[string]string
	entities map[string]string

	current  string
	toggleMu sync.Mutex

	store    ModuleStore
	storeErr error
	fanout   fanout.Fanout
	nodeID   string
	db       *sql.DB

	processCoordinator processModuleCoordinator
	process            map[string]bool

	auditToggle func(context.Context, string, bool)
}

// NewModuleManager creates a manager on a SQLModuleStore when db is
// non-nil, else in memory. A SQL store that cannot be created fails the
// boot, at loadFromStore.
func NewModuleManager(db *sql.DB, f fanout.Fanout) *ModuleManager {
	mm := &ModuleManager{
		modules:  make(map[string]*moduleEntry),
		enabled:  make(map[string]bool),
		routes:   make(map[string]string),
		tools:    make(map[string]string),
		entities: make(map[string]string),
		process:  make(map[string]bool),
		fanout:   f,
		nodeID:   fanout.NewNodeID(),
		db:       db,
	}
	if db != nil {
		store, err := NewSQLModuleStore(db)
		if err == nil {
			mm.store = store
		} else {
			mm.storeErr = fmt.Errorf("create modules table: %w", err)
		}
	} else {
		mm.store = NewInMemoryModuleStore()
	}
	return mm
}

func (mm *ModuleManager) register(name string, manifest ModuleManifest) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.modules[name] = &moduleEntry{manifest: manifest}
	mm.order = append(mm.order, name)
}

func (mm *ModuleManager) setCurrent(name string) {
	mm.mu.Lock()
	mm.current = name
	mm.mu.Unlock()
}

func (mm *ModuleManager) clearCurrent() {
	mm.mu.Lock()
	mm.current = ""
	mm.mu.Unlock()
}

func (mm *ModuleManager) currentModule() string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.current
}

func (mm *ModuleManager) recordRoute(method, pattern string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.routes[method+" "+pattern] = mm.current
	}
	mm.mu.Unlock()
}

func (mm *ModuleManager) recordTool(toolName string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.tools[toolName] = mm.current
	}
	mm.mu.Unlock()
}

func (mm *ModuleManager) recordEntity(entityName string) {
	mm.mu.Lock()
	if mm.current != "" {
		mm.entities[entityName] = mm.current
	}
	mm.mu.Unlock()
}

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

// Enabled reports whether name is enabled; a module with no stored state
// is. A process module answers from its supervisor's desired state.
func (mm *ModuleManager) Enabled(name string) bool {
	mm.mu.RLock()
	if mm.process[name] {
		pt, _ := mm.processCoordinator.(processModuleToggler)
		mm.mu.RUnlock()
		return pt != nil && pt.DesiredEnabled(name)
	}
	defer mm.mu.RUnlock()
	v, ok := mm.enabled[name]
	if !ok {
		return true
	}
	return v
}

func (mm *ModuleManager) routeAllowed(key string) bool {
	mm.mu.RLock()
	owner := mm.routes[key]
	mm.mu.RUnlock()
	if owner == "" {
		return true
	}
	return mm.Enabled(owner)
}

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

func (mm *ModuleManager) cronGate(moduleName string) func(string) bool {
	return func(_ string) bool {
		return mm.Enabled(moduleName)
	}
}

func (mm *ModuleManager) queueGate(moduleName string) func(string) bool {
	return func(_ string) bool {
		return mm.Enabled(moduleName)
	}
}

func (mm *ModuleManager) hasModule(name string) bool {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	_, ok := mm.modules[name]
	return ok
}

func (mm *ModuleManager) dependents(name string) []string {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	var deps []string
	for _, modName := range mm.order {
		entry := mm.modules[modName]
		if entry == nil {
			continue
		}
		if slices.Contains(entry.manifest.DependsOn, name) {
			if mm.enabled[modName] {
				deps = append(deps, modName)
			}
		}
	}
	return deps
}

// Enable persists and applies name's enabled state. It errors when name
// is not registered or one of its DependsOn is unregistered or disabled.
func (mm *ModuleManager) Enable(ctx context.Context, name string) (err error) {
	defer func() {
		if err == nil && mm.auditToggle != nil {
			mm.auditToggle(ctx, name, true)
		}
	}()
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
	if err := mm.setEnabled(ctx, name, true); err != nil {
		return fmt.Errorf("persist enable for %q: %w", name, err)
	}
	mm.publish(ctx, name, true)
	return nil
}

func (mm *ModuleManager) setEnabled(ctx context.Context, name string, enabled bool) error {
	mm.mu.RLock()
	isProcess := mm.process[name]
	pt, _ := mm.processCoordinator.(processModuleToggler)
	mm.mu.RUnlock()
	if isProcess {
		if pt == nil {
			return fmt.Errorf("process module %q has no supervisor", name)
		}
		return pt.SetDesiredEnabled(ctx, name, enabled)
	}
	if err := mm.store.SetEnabled(ctx, name, enabled); err != nil {
		return err
	}
	mm.mu.Lock()
	mm.enabled[name] = enabled
	mm.mu.Unlock()
	return nil
}

// Disable persists and applies name's disabled state. It errors when name
// is not registered or an enabled module depends on it; it never cascades.
func (mm *ModuleManager) Disable(ctx context.Context, name string) (err error) {
	defer func() {
		if err == nil && mm.auditToggle != nil {
			mm.auditToggle(ctx, name, false)
		}
	}()
	mm.toggleMu.Lock()
	defer mm.toggleMu.Unlock()

	if !mm.hasModule(name) {
		return fmt.Errorf("module %q is not registered", name)
	}
	deps := mm.dependents(name)
	if len(deps) > 0 {
		return fmt.Errorf("cannot disable module %q: enabled modules %v depend on it", name, deps)
	}
	if err := mm.setEnabled(ctx, name, false); err != nil {
		return fmt.Errorf("persist disable for %q: %w", name, err)
	}
	mm.publish(ctx, name, false)
	return nil
}

type moduleToggleMessage struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (mm *ModuleManager) publish(_ context.Context, name string, enabled bool) {
	if mm.fanout == nil {
		return
	}
	payload, _ := json.Marshal(moduleToggleMessage{Name: name, Enabled: enabled})
	envelope := fanout.Wrap(mm.nodeID, payload)
	if err := mm.fanout.Publish(context.Background(), "gofastr.modules", envelope); err != nil {
		log.Printf("WARN: module manager: fanout publish for %q failed: %v: other replicas may be stale until restart", name, err)
	}
}

func (mm *ModuleManager) handleRemoteToggle(raw []byte) {
	nodeID, body, err := fanout.Unwrap(raw)
	if err != nil {
		return
	}
	if nodeID == mm.nodeID {
		return
	}
	var msg moduleToggleMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return
	}
	if !mm.hasModule(msg.Name) {
		return
	}
	state, err := mm.store.Load(context.Background())
	if err != nil {
		log.Printf("WARN: module manager: store load on fanout refresh for %q failed: %v: keeping current cache", msg.Name, err)
		return
	}
	mm.mu.Lock()
	if v, ok := state[msg.Name]; ok {
		mm.enabled[msg.Name] = v
	}
	pc := mm.processCoordinator
	mm.mu.Unlock()
	if pc != nil {
		pc.Reconcile(msg.Name)
	}
}

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
	pc := mm.processCoordinator
	process := maps.Clone(mm.process)
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
	mm.mu.RUnlock()

	pt, _ := pc.(processModuleToggler)
	for i := range out {
		info := &out[i]
		if process[info.Name] {
			info.Enabled = pt != nil && pt.DesiredEnabled(info.Name)
		}
		if pc == nil {
			continue
		}
		if pi, ok := pc.LocalInfo(info.Name); ok {
			info.ProcessState = pi.State.String()
			info.RestartCount = pi.RestartCount
			info.ObservedGeneration = pi.ObservedGeneration
			info.InstanceID = pi.InstanceID
			info.LastExit = pi.LastExit
		}
	}
	return out
}

type processModuleCoordinator interface {
	Reconcile(name string)
	LocalInfo(name string) (ProcessModuleInfo, bool)
}

type processModuleToggler interface {
	DesiredEnabled(name string) bool
	SetDesiredEnabled(ctx context.Context, name string, enabled bool) error
}

func (mm *ModuleManager) markProcess(name string) {
	mm.mu.Lock()
	mm.process[name] = true
	mm.mu.Unlock()
}

// SetProcessCoordinator installs, or with nil removes, the process-module
// coordinator, replacing any earlier one.
func (mm *ModuleManager) SetProcessCoordinator(pc processModuleCoordinator) {
	mm.mu.Lock()
	mm.processCoordinator = pc
	mm.mu.Unlock()
}

// RegisterModule registers m as a battery depending on its DependsOn and
// records its manifest. It panics after InitPlugins and on a battery
// registration error.
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

// Modules returns the app's module manager.
func (a *App) Modules() *ModuleManager {
	return a.modules
}
