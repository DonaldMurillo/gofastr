package framework

import (
	"fmt"

	"github.com/gofastr/gofastr/core/router"
)

// Plugin is the base interface for GoFastr plugins.
type Plugin interface {
	// Name returns the unique plugin identifier.
	Name() string
	// Init initializes the plugin with access to the App.
	Init(app *App) error
}

// HasRoutes is an optional interface for plugins that register routes.
type HasRoutes interface {
	RegisterRoutes(r *router.Router)
}

// HasMiddleware is an optional interface for plugins that add middleware.
type HasMiddleware interface {
	RegisterMiddleware(app *App)
}

// HasHooks is an optional interface for plugins that register lifecycle hooks.
type HasHooks interface {
	RegisterHooks(app *App)
}

// PluginManager manages registered plugins.
type PluginManager struct {
	plugins map[string]Plugin
	order   []string // insertion order
}

// NewPluginManager creates a new plugin manager.
func NewPluginManager() *PluginManager {
	return &PluginManager{
		plugins: make(map[string]Plugin),
	}
}

// Register adds a plugin to the manager.
// Returns an error if a plugin with the same name is already registered.
func (pm *PluginManager) Register(plugin Plugin) error {
	name := plugin.Name()
	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}
	pm.plugins[name] = plugin
	pm.order = append(pm.order, name)
	return nil
}

// InitAll initializes all plugins in registration order.
func (pm *PluginManager) InitAll(app *App) error {
	for _, name := range pm.order {
		plugin := pm.plugins[name]
		if err := plugin.Init(app); err != nil {
			return fmt.Errorf("plugin %q init failed: %w", name, err)
		}
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

// RegisterRoutes calls RegisterRoutes on all plugins that implement HasRoutes.
func (pm *PluginManager) RegisterRoutes(r *router.Router) {
	for _, name := range pm.order {
		if rp, ok := pm.plugins[name].(HasRoutes); ok {
			rp.RegisterRoutes(r)
		}
	}
}

// RegisterMiddleware calls RegisterMiddleware on all plugins that implement HasMiddleware.
func (pm *PluginManager) RegisterMiddleware(app *App) {
	for _, name := range pm.order {
		if mp, ok := pm.plugins[name].(HasMiddleware); ok {
			mp.RegisterMiddleware(app)
		}
	}
}

// RegisterHooks calls RegisterHooks on all plugins that implement HasHooks.
func (pm *PluginManager) RegisterHooks(app *App) {
	for _, name := range pm.order {
		if hp, ok := pm.plugins[name].(HasHooks); ok {
			hp.RegisterHooks(app)
		}
	}
}
