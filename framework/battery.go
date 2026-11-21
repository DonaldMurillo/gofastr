package framework

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sort"
	"sync"
)

// Battery is a Plugin that declares dependencies at registration, and
// can implement BatteryLifecycle for start and stop hooks of its own.
// Batteries init in dependency order.
type Battery interface {
	// Name returns the unique battery identifier.
	Name() string
	// Init wires the battery into the App, once, after its dependencies'.
	Init(app *App) error
}

// BatteryLifecycle is the optional interface for batteries that need to
// participate in the App's startup and shutdown sequence.
type BatteryLifecycle interface {
	Battery

	// OnStart runs after every Init, before the server accepts
	// connections; ctx is cancelled at shutdown. An error aborts startup.
	OnStart(ctx context.Context) error
	// OnStop runs at shutdown after the server stops, dependents first.
	OnStop(ctx context.Context) error
}

type batteryEntry struct {
	battery     Battery
	deps        []string
	initOrder   int
	initialized bool
}

// BatteryManager manages registered batteries, resolves dependencies, and
// orchestrates initialization and lifecycle.
type BatteryManager struct {
	entries map[string]*batteryEntry
	order   []string
	sorted  []string
	mu      sync.RWMutex
	logger  func() *slog.Logger
}

func (bm *BatteryManager) log() *slog.Logger {
	if bm.logger == nil {
		return nil
	}
	return bm.logger()
}

// NewBatteryManager creates a new BatteryManager.
func NewBatteryManager() *BatteryManager {
	return &BatteryManager{
		entries: make(map[string]*batteryEntry),
	}
}

// Register adds a battery and the names of the batteries it depends on.
// It errors on a nil battery, an invalid name or a duplicate one; an
// unknown dependency fails InitAll.
func (bm *BatteryManager) Register(b Battery, deps ...string) error {
	if b == nil {
		return fmt.Errorf("battery: cannot register nil battery")
	}
	name := b.Name()
	if err := validModuleName(name); err != nil {
		return fmt.Errorf("battery: invalid name %q: %w", name, err)
	}
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
	b, err := bm.Get(name)
	return getTyped[T]("battery", name, b, err)
}

func getTyped[T any](kind, name string, v any, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	typed, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("%s %q does not implement %T", kind, name, zero)
	}
	return typed, nil
}

func (bm *BatteryManager) resolveOrder() error {
	for name, entry := range bm.entries {
		for _, dep := range entry.deps {
			if _, ok := bm.entries[dep]; !ok {
				return fmt.Errorf("battery %q depends on unknown battery %q", name, dep)
			}
		}
	}

	inDegree := make(map[string]int)
	adj := make(map[string][]string)

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
		var unresolved []string
		for name, deg := range inDegree {
			if deg > 0 {
				unresolved = append(unresolved, name)
			}
		}
		return fmt.Errorf("circular battery dependency involving: %v", unresolved)
	}

	bm.mu.Lock()
	bm.sorted = sorted
	bm.mu.Unlock()
	return nil
}

func (bm *BatteryManager) resolved() []string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.sorted
}

// InitAll initializes, in dependency order, every battery not yet
// initialized. It errors on an unknown or circular dependency, and on a
// failing or panicking Init, naming the battery.
func (bm *BatteryManager) InitAll(app *App) error {
	if err := bm.resolveOrder(); err != nil {
		return err
	}
	for i, name := range bm.resolved() {
		entry := bm.entries[name]
		entry.initOrder = i
		if entry.initialized {
			continue
		}
		if app.modules != nil {
			if _, isMod := entry.battery.(Module); isMod {
				app.modules.setCurrent(name)
			} else {
				app.modules.clearCurrent()
			}
		}
		if err := callModuleSafe(app.logger.Load(), "battery", name, "init", func() error { return entry.battery.Init(app) }); err != nil {
			return err
		}
		entry.initialized = true
	}
	if app.modules != nil {
		app.modules.clearCurrent()
	}
	return nil
}

func callModuleSafe(log *slog.Logger, kind, name, phase string, call func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = recoveredPanic(log, fmt.Sprintf("%s %q %s", kind, name, phase), fmt.Sprintf("%T", v))
		}
	}()
	if e := call(); e != nil {
		return fmt.Errorf("%s %q %s failed: %w", kind, name, phase, e)
	}
	return nil
}

// recoveredPanic logs the stack and returns the error; call it from the
// deferred recover, passing the panic value's type name, never the value.
func recoveredPanic(log *slog.Logger, what, panicType string) error {
	if log == nil {
		log = slog.Default()
	}
	log.Error("framework: recovered panic", "in", what, "panic_type", panicType, "stack", string(debug.Stack()))
	if loc, ok := log.Handler().(interface{ LogLocation() string }); ok {
		if where := loc.LogLocation(); where != "" {
			return fmt.Errorf("%s panicked (panic type %s); its stack is logged to %q", what, panicType, where)
		}
	}
	return fmt.Errorf("%s panicked (panic type %s); its stack is logged", what, panicType)
}

// StartAll calls OnStart in dependency order and returns the first
// error or recovered panic, naming the battery.
func (bm *BatteryManager) StartAll(ctx context.Context) error {
	for _, name := range bm.resolved() {
		if lc, ok := bm.entries[name].battery.(BatteryLifecycle); ok {
			if err := callModuleSafe(bm.log(), "battery", name, "start", func() error { return lc.OnStart(ctx) }); err != nil {
				return err
			}
		}
	}
	return nil
}

// StopAll calls OnStop in reverse dependency order, stopping every
// battery even after one fails or panics, and returns the joined errors.
func (bm *BatteryManager) StopAll(ctx context.Context) error {
	var errs []error
	for _, name := range slices.Backward(bm.resolved()) {
		if lc, ok := bm.entries[name].battery.(BatteryLifecycle); ok {
			if err := callModuleSafe(bm.log(), "battery", name, "stop", func() error { return lc.OnStop(ctx) }); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// All returns all registered batteries in dependency-resolved order.
func (bm *BatteryManager) All() []Battery {
	sorted := bm.resolved()
	result := make([]Battery, 0, len(sorted))
	for _, name := range sorted {
		result = append(result, bm.entries[name].battery)
	}
	return result
}

// registered is every battery in registration order; it needs no InitAll.
func (bm *BatteryManager) registered() []Battery {
	out := make([]Battery, 0, len(bm.order))
	for _, name := range bm.order {
		out = append(out, bm.entries[name].battery)
	}
	return out
}

// Names returns battery names in dependency-resolved order.
func (bm *BatteryManager) Names() []string {
	return append([]string{}, bm.resolved()...)
}
