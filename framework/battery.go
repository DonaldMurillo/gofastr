package framework

import (
	"context"
	"fmt"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// Battery is the interface for heavyweight, lifecycle-aware modules that
// extend a GoFastr application (auth, search, cache, etc.). It extends the
// lightweight Plugin with startup/shutdown hooks, dependency ordering, and
// first-class integration with the App spine.
//
// Batteries are registered via App.RegisterBattery and initialized during
// App.Start in dependency order. The auth battery under battery/auth is the
// canonical example.
//
// Migration path: existing Plugin implementations keep working unchanged.
// Only modules that need lifecycle hooks or dependency ordering should
// implement Battery instead.
type Battery interface {
	// Name returns the unique battery identifier. Used for dependency
	// resolution and logging.
	Name() string

	// Init initializes the battery with access to the App. Called once
	// during App.Start, before any lifecycle hooks fire. Dependencies
	// are guaranteed to be initialized before dependents.
	// Accepts interface{} so batteries in separate packages (e.g. battery/auth)
	// can implement it without importing framework (avoiding circular deps).
	// The caller always passes a concrete *App.
	Init(app interface{}) error
}

// BatteryLifecycle is the optional interface for batteries that need to
// participate in the App's startup and shutdown sequence.
type BatteryLifecycle interface {
	Battery

	// OnStart is called after all batteries are initialized and before the
	// HTTP server begins accepting connections. The context is cancelled
	// when the app shuts down, so long-running workers should respect it.
	// The first error aborts startup.
	OnStart(ctx context.Context) error

	// OnStop is called during graceful shutdown, after the HTTP server has
	// stopped. Batteries stop in reverse dependency order (dependents
	// first, then their dependencies).
	OnStop(ctx context.Context) error
}

// BatteryRoutes is the optional interface for batteries that register HTTP routes.
type BatteryRoutes interface {
	Battery
	RegisterRoutes(r *router.Router)
}

// BatteryMiddleware is the optional interface for batteries that provide middleware.
type BatteryMiddleware interface {
	Battery
	RegisterMiddleware(app *App)
}

// BatteryHooks is the optional interface for batteries that register entity
// lifecycle hooks.
type BatteryHooks interface {
	Battery
	RegisterHooks(app *App)
}

// BatteryTools is the optional interface for batteries that register MCP tools.
type BatteryTools interface {
	Battery
	RegisterTools(server *mcp.Server)
}

// BatteryConfig holds metadata about a registered battery.
type batteryEntry struct {
	battery   Battery
	deps      []string // names of batteries this one depends on
	config    any      // optional battery-specific config
	initOrder int      // set during topological sort
}

// BatteryManager manages registered batteries, resolves dependencies, and
// orchestrates initialization and lifecycle.
type BatteryManager struct {
	entries map[string]*batteryEntry
	order   []string // registration order
	sorted  []string // dependency-resolved order (computed)
}

// NewBatteryManager creates a new BatteryManager.
func NewBatteryManager() *BatteryManager {
	return &BatteryManager{
		entries: make(map[string]*batteryEntry),
	}
}

// Register adds a battery with optional dependency declarations.
// Deps lists battery names that must be initialized before this one.
// Returns an error on duplicate name or unknown dependency.
func (bm *BatteryManager) Register(b Battery, deps ...string) error {
	if b == nil {
		return fmt.Errorf("battery: cannot register nil battery")
	}
	name := b.Name()
	if _, exists := bm.entries[name]; exists {
		return fmt.Errorf("battery %q already registered", name)
	}
	bm.entries[name] = &batteryEntry{
		battery: b,
		deps:    deps,
	}
	bm.order = append(bm.order, name)
	return nil
}

// Get retrieves a battery by name. Returns the Battery interface
// so callers can type-assert to the concrete type or any optional interface.
func (bm *BatteryManager) Get(name string) (Battery, error) {
	entry, ok := bm.entries[name]
	if !ok {
		return nil, fmt.Errorf("battery %q not found", name)
	}
	return entry.battery, nil
}

// GetAs retrieves a battery by name and type-asserts it to T.
// Returns an error if the battery is not found or doesn't implement T.
func GetAs[T any](bm *BatteryManager, name string) (T, error) {
	var zero T
	b, err := bm.Get(name)
	if err != nil {
		return zero, err
	}
	typed, ok := b.(T)
	if !ok {
		return zero, fmt.Errorf("battery %q does not implement %T", name, zero)
	}
	return typed, nil
}

// resolveOrder performs a topological sort of batteries based on dependencies.
// Returns an error on missing dependencies or circular references.
func (bm *BatteryManager) resolveOrder() error {
	// Verify all deps reference registered batteries
	for name, entry := range bm.entries {
		for _, dep := range entry.deps {
			if _, ok := bm.entries[dep]; !ok {
				return fmt.Errorf("battery %q depends on unknown battery %q", name, dep)
			}
		}
	}

	// Kahn's algorithm for topological sort
	inDegree := make(map[string]int)
	adj := make(map[string][]string) // dep → dependents

	for name := range bm.entries {
		inDegree[name] = 0
	}
	for name, entry := range bm.entries {
		inDegree[name] = len(entry.deps)
		for _, dep := range entry.deps {
			adj[dep] = append(adj[dep], name)
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	// Sort the initial queue for deterministic ordering among equal-priority
	sort.Strings(queue)

	var sorted []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		sorted = append(sorted, curr)

		dependents := adj[curr]
		sort.Strings(dependents)
		for _, dep := range dependents {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	if len(sorted) != len(bm.entries) {
		// Circular dependency — find which batteries are unresolved
		var unresolved []string
		for name, deg := range inDegree {
			if deg > 0 {
				unresolved = append(unresolved, name)
			}
		}
		return fmt.Errorf("circular battery dependency involving: %v", unresolved)
	}

	bm.sorted = sorted
	return nil
}

// InitAll initializes all batteries in dependency order. Called during
// App.Start before the HTTP server binds.
func (bm *BatteryManager) InitAll(app *App) error {
	if err := bm.resolveOrder(); err != nil {
		return err
	}
	for i, name := range bm.sorted {
		entry := bm.entries[name]
		entry.initOrder = i
		if err := entry.battery.Init(app); err != nil {
			return fmt.Errorf("battery %q init failed: %w", name, err)
		}
	}
	return nil
}

// StartAll calls OnStart on batteries that implement BatteryLifecycle,
// in dependency order (dependencies first).
func (bm *BatteryManager) StartAll(ctx context.Context) error {
	for _, name := range bm.sorted {
		if lc, ok := bm.entries[name].battery.(BatteryLifecycle); ok {
			if err := lc.OnStart(ctx); err != nil {
				return fmt.Errorf("battery %q start failed: %w", name, err)
			}
		}
	}
	return nil
}

// StopAll calls OnStop on batteries that implement BatteryLifecycle,
// in reverse dependency order (dependents first, then dependencies).
func (bm *BatteryManager) StopAll(ctx context.Context) error {
	var firstErr error
	for i := len(bm.sorted) - 1; i >= 0; i-- {
		name := bm.sorted[i]
		if lc, ok := bm.entries[name].battery.(BatteryLifecycle); ok {
			if err := lc.OnStop(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("battery %q stop failed: %w", name, err)
			}
		}
	}
	return firstErr
}

// RegisterRoutes calls RegisterRoutes on batteries that implement BatteryRoutes,
// in dependency order.
func (bm *BatteryManager) RegisterRoutes(r *router.Router) {
	for _, name := range bm.sorted {
		if br, ok := bm.entries[name].battery.(BatteryRoutes); ok {
			br.RegisterRoutes(r)
		}
	}
}

// RegisterMiddleware calls RegisterMiddleware on batteries that implement BatteryMiddleware.
func (bm *BatteryManager) RegisterMiddleware(app *App) {
	for _, name := range bm.sorted {
		if bm, ok := bm.entries[name].battery.(BatteryMiddleware); ok {
			bm.RegisterMiddleware(app)
		}
	}
}

// RegisterHooks calls RegisterHooks on batteries that implement BatteryHooks.
func (bm *BatteryManager) RegisterHooks(app *App) {
	for _, name := range bm.sorted {
		if bh, ok := bm.entries[name].battery.(BatteryHooks); ok {
			bh.RegisterHooks(app)
		}
	}
}

// RegisterTools calls RegisterTools on batteries that implement BatteryTools.
func (bm *BatteryManager) RegisterTools(server *mcp.Server) {
	for _, name := range bm.sorted {
		if bt, ok := bm.entries[name].battery.(BatteryTools); ok {
			bt.RegisterTools(server)
		}
	}
}

// All returns all registered batteries in dependency-resolved order.
func (bm *BatteryManager) All() []Battery {
	result := make([]Battery, 0, len(bm.sorted))
	for _, name := range bm.sorted {
		result = append(result, bm.entries[name].battery)
	}
	return result
}

// Names returns battery names in dependency-resolved order.
func (bm *BatteryManager) Names() []string {
	return append([]string{}, bm.sorted...)
}
