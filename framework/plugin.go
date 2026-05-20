package framework

import (
	"fmt"
	"strings"
	"unicode"
)

// maxModuleNameLen caps Plugin / Battery names. Above this length the
// Register call rejects the registration — names appear in error
// messages and structured log entries, so unbounded values become
// log-volume amplification.
const maxModuleNameLen = 128

// validModuleName rejects names that would be invisible or confusing
// in errors: empty, whitespace-only, containing control characters,
// or longer than maxModuleNameLen.
func validModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if len(name) > maxModuleNameLen {
		return fmt.Errorf("name is %d chars (max %d)", len(name), maxModuleNameLen)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is whitespace-only")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("name contains a control character")
		}
	}
	return nil
}

// Plugin is the interface for lightweight GoFastr plugins.
//
// Plugins have a single integration point: Init(app). From there a plugin
// does everything it needs by calling into the App — register routes via
// app.Router, add middleware via app.Use, swap the logger via
// app.SetLogger, register MCP tools via app.MCP, attach hooks via
// app.HookRegistry(name).Add..., and so on.
//
// There are no optional interfaces. The router resolves middleware
// late-bound, so middleware added from Init wraps routes registered
// before the plugin loaded — there is no ordering footgun to dodge.
type Plugin interface {
	// Name returns the unique plugin identifier.
	Name() string
	// Init wires the plugin into the App. Called once during App.Start.
	Init(app *App) error
}

// PluginManager manages registered plugins.
type PluginManager struct {
	plugins     map[string]Plugin
	order       []string // insertion order
	initialized map[string]bool
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins:     make(map[string]Plugin),
		initialized: make(map[string]bool),
	}
}

// Register adds a plugin to the manager.
// Returns an error if a plugin with the same name is already registered,
// or if the name is invalid (empty, whitespace-only, contains a control
// character, or longer than maxModuleNameLen).
func (pm *PluginManager) Register(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin: cannot register nil plugin")
	}
	name := plugin.Name()
	if err := validModuleName(name); err != nil {
		return fmt.Errorf("plugin: invalid name %q: %w", name, err)
	}
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}
	pm.plugins[name] = plugin
	pm.order = append(pm.order, name)
	return nil
}

// InitAll initializes all plugins in registration order.
//
// Wraps each Init in a deferred recover so a panic — most commonly
// `http: multiple registrations for ...` from a duplicate route
// pattern — gets tagged with the offending plugin's name. Without
// this the panic surfaces deep in ServeMux with no context about
// which plugin registered the conflicting route.
//
// Tracks per-plugin init state so that a retry after partial failure
// (App.InitPlugins rolls back its global latch on error) only re-runs
// plugins that haven't already applied side effects. Without this a
// retry would re-Register the routes the successful plugins already
// added and panic on the ServeMux duplicate-pattern check.
func (pm *PluginManager) InitAll(app *App) error {
	for _, name := range pm.order {
		if pm.initialized[name] {
			continue
		}
		plugin := pm.plugins[name]
		if err := initPluginSafe(name, plugin, app); err != nil {
			return err
		}
		pm.initialized[name] = true
	}
	return nil
}

func initPluginSafe(name string, plugin Plugin, app *App) (err error) {
	defer func() {
		if v := recover(); v != nil {
			// Format with %T not %v: a plugin that does panic(config)
			// where config holds an API key would otherwise leak the
			// secret into every operator log via this error string.
			// Operators wanting the full panic value can set
			// GOTRACEBACK=all and read the stack.
			err = fmt.Errorf("plugin %q init panicked (panic type %T) — set GOTRACEBACK=all for details", name, v)
		}
	}()
	if e := plugin.Init(app); e != nil {
		return fmt.Errorf("plugin %q init failed: %w", name, e)
	}
	return nil
}

// Get retrieves a plugin by name.
func (pm *PluginManager) Get(name string) (Plugin, error) {
	p, ok := pm.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", name)
	}
	return p, nil
}

// All returns all registered plugins in order.
func (pm *PluginManager) All() []Plugin {
	result := make([]Plugin, 0, len(pm.order))
	for _, name := range pm.order {
		result = append(result, pm.plugins[name])
	}
	return result
}

// Names returns the names of all registered plugins in registration order.
func (pm *PluginManager) Names() []string {
	return append([]string{}, pm.order...)
}
