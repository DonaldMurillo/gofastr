// Package plugin defines the Plugin interface. Plugins are
// compile-time: a plugin author imports the harness as a library,
// implements Plugin, and links it into a custom binary. Go's
// plugin.Open is rejected for ABI fragility (see § Plugin
// distribution model). The recommended distribution channel for
// third-party tools is an MCP server bridged via mcpclient.
package plugin

// Plugin is the single extension contract. On Register, a plugin
// attaches middleware, subscribes to events, registers backends, and
// claims slash-command namespaces.
//
// The Host is the subset of the harness API a plugin may touch;
// concrete *harness.Harness satisfies it. Defining Host as an
// interface in this package keeps plugin/ free of the harness/
// import (which would create a cycle).
type Plugin interface {
	Name() string
	Register(h Host) error
}

// Host is the API surface plugins access. Concrete harness.Harness
// satisfies this; we declare only what's needed so the interface
// doesn't drift.
//
// Subsystem-shaped methods take the subsystem's package-level type;
// this package re-declares minimal interfaces below to avoid heavy
// imports.
type Host interface {
	// ClaimSlashCommand reserves a namespace prefix for this
	// plugin's commands (e.g. "custom" for `/custom:foo`). Returns
	// an error on conflict with built-in or another plugin.
	ClaimSlashCommand(namespace string) error

	// AddRequestMiddleware appends a RequestMiddleware to the
	// engine's chain. The host wires it into every per-session
	// engine that boots after this point.
	AddRequestMiddleware(mw RequestMiddleware)

	// AddToolMiddleware appends a ToolMiddleware to the dispatcher
	// chain.
	AddToolMiddleware(mw ToolMiddleware)

	// RegisterToolSource adds a ToolSource (e.g. plugin-defined
	// tools) to the registry.
	RegisterToolSource(src ToolSource) error

	// RegisterProvider adds an LLM provider implementation.
	RegisterProvider(p Provider) error

	// SubscribeEvents returns a channel of canonical event
	// envelopes for cross-session subscribers (cost dashboards,
	// telemetry, etc.).
	SubscribeEvents() <-chan EventEnvelope
}

// Indirected types — these mirror the concrete subsystem types so
// plugin/ remains import-light. The harness composition layer wires
// concrete implementations behind these.

type RequestMiddleware = any // engine.RequestMiddleware
type ToolMiddleware = any    // tool.Middleware
type ToolSource = any        // tool.ToolSource
type Provider = any          // provider.Provider
type EventEnvelope = any     // control.EventEnvelope

// Manager holds the registered plugins for a harness process and
// drives Register on boot.
type Manager struct {
	plugins []Plugin
}

// NewManager returns an empty Manager.
func NewManager() *Manager { return &Manager{} }

// Register adds a plugin. Order matters: plugins registered earlier
// see their middleware run before later ones in the chain.
func (m *Manager) Register(p Plugin) {
	m.plugins = append(m.plugins, p)
}

// InitAll calls Register(h) on every registered plugin in registration
// order. Returns the first error.
func (m *Manager) InitAll(h Host) error {
	for _, p := range m.plugins {
		if err := p.Register(h); err != nil {
			return err
		}
	}
	return nil
}

// List returns the plugin names in registration order.
func (m *Manager) List() []string {
	out := make([]string, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p.Name())
	}
	return out
}
