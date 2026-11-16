package framework

import (
	"fmt"
	"strings"
	"unicode"
)

const maxModuleNameLen = 128

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

// Plugin is an extension with no dependencies and no lifecycle of its own.
// Init wires it into the App (routes, middleware, hooks, MCP tools).
type Plugin interface {
	// Name returns the unique plugin identifier.
	Name() string
	// Init wires the plugin into the App. Called once during App.Start.
	Init(app *App) error
}

// PluginManager manages registered plugins.
type PluginManager struct {
	plugins     map[string]Plugin
	order       []string
	initialized map[string]bool
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins:     make(map[string]Plugin),
		initialized: make(map[string]bool),
	}
}

// Register adds a plugin. It errors on a nil plugin, a duplicate name, or
// a name that is empty, whitespace-only, holds a control character or is
// longer than 128 bytes.
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

// InitAll initializes, in registration order, every plugin not yet
// initialized. A failing or panicking Init returns an error naming it.
func (pm *PluginManager) InitAll(app *App) error {
	for _, name := range pm.order {
		if pm.initialized[name] {
			continue
		}
		plugin := pm.plugins[name]
		if err := callModuleSafe(app.logger.Load(), "plugin", name, "init", func() error { return plugin.Init(app) }); err != nil {
			return err
		}
		pm.initialized[name] = true
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

// PluginGetAs retrieves a plugin by name and type-asserts it to T.
// Returns an error if the plugin is not found or doesn't implement T.
func PluginGetAs[T any](pm *PluginManager, name string) (T, error) {
	p, err := pm.Get(name)
	return getTyped[T]("plugin", name, p, err)
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
