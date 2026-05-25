// Package resources aggregates read-only catalogs (sessions,
// profiles, providers, tools, skills, mcp servers, slash-commands,
// runtime stats) for REST and mcpserver to surface.
//
// Per § Control plane → Package map, this layer sits above
// session/skill/tool/profile and below the transport so transports
// don't need to back-import everything.
package resources

import (
	"context"
	"sort"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/harness/slash"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Catalog is the aggregator. The harness composition layer wires the
// subsystem references (Engines, Tools, Skills, Providers, etc.)
// once at boot; transports read from this surface.
type Catalog struct {
	mu sync.RWMutex

	// engines is the live set; one per session.
	engines map[ids.SessionID]*engine.Engine

	// pluginNamespaces is the union of slash-command namespaces
	// claimed by plugins. Reserved built-in namespaces come from
	// slash.AllNamespaces().
	pluginNamespaces []string

	// Tools is the registered tool catalog.
	Tools *tool.Registry

	// Providers is the configured provider list. Order is
	// registration order.
	Providers []provider.Provider

	// Skills returns the loaded skill tier-1 metadata. The
	// composition wires a callable so the catalog stays decoupled
	// from skill.Registry's concrete type.
	Skills func() []skillmd.Tier1
}

// NewCatalog returns an empty Catalog. Wire fields after construction.
func NewCatalog() *Catalog {
	return &Catalog{
		engines: make(map[ids.SessionID]*engine.Engine),
	}
}

// RegisterEngine registers a session's engine for inclusion in
// /v1/sessions listings.
func (c *Catalog) RegisterEngine(e *engine.Engine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.engines[e.Session] = e
}

// UnregisterEngine removes a session.
func (c *Catalog) UnregisterEngine(s ids.SessionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.engines, s)
}

// ClaimNamespace adds a plugin-claimed slash-command namespace.
func (c *Catalog) ClaimNamespace(ns string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pluginNamespaces = append(c.pluginNamespaces, ns)
}

// ----- DTOs surfaced over the wire -----

// SessionInfo is the shape /v1/sessions and harness/v1://sessions return.
type SessionInfo struct {
	SessionID ids.SessionID `json:"session"`
	Profile   string        `json:"profile"`
	Model     string        `json:"model"`
	Provider  string        `json:"provider"`
	Turns     int           `json:"turns"`
}

// ProviderInfo is the shape /v1/providers returns.
type ProviderInfo struct {
	Name   string           `json:"name"`
	Models []provider.Model `json:"models"`
}

// ToolInfo is the shape /v1/tools returns.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mutating    bool   `json:"mutating"`
	Source      string `json:"source"`
	Schema      []byte `json:"schema"`
}

// SlashCommandInfo is the shape /v1/slash-commands returns.
type SlashCommandInfo struct {
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ArgsHelp    string `json:"args_help,omitempty"`
	IsBuiltin   bool   `json:"is_builtin"`
}

// ListSessions returns active sessions, sorted by SessionID.
func (c *Catalog) ListSessions() []SessionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]SessionInfo, 0, len(c.engines))
	for _, e := range c.engines {
		out = append(out, SessionInfo{
			SessionID: e.Session,
			Model:     e.Model,
			Provider:  e.Provider.Name(),
			Turns:     countAssistant(e),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

// ListProviders returns the provider catalog with model details.
func (c *Catalog) ListProviders(ctx context.Context) []ProviderInfo {
	out := make([]ProviderInfo, 0, len(c.Providers))
	for _, p := range c.Providers {
		models, _ := p.Models(ctx)
		out = append(out, ProviderInfo{Name: p.Name(), Models: models})
	}
	return out
}

// ListTools returns the registered tool catalog.
func (c *Catalog) ListTools() []ToolInfo {
	if c.Tools == nil {
		return nil
	}
	tools := c.Tools.List()
	out := make([]ToolInfo, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Mutating:    t.Mutating(),
			Source:      c.Tools.SourceOf(t.Name()),
			Schema:      t.InputSchema(),
		})
	}
	return out
}

// ListSlashCommands returns the union of built-in and plugin commands.
func (c *Catalog) ListSlashCommands() []SlashCommandInfo {
	out := make([]SlashCommandInfo, 0, 32)
	// Built-ins (no namespace).
	for _, b := range slash.AllBuiltins() {
		out = append(out, SlashCommandInfo{
			Name: b.Name, Description: b.Description, ArgsHelp: b.ArgsHelp, IsBuiltin: true,
		})
	}
	// Reserved namespaces (without specific verbs — caller can
	// enumerate skills, profiles, etc. via dedicated endpoints).
	for _, ns := range slash.AllNamespaces() {
		out = append(out, SlashCommandInfo{
			Namespace:   ns,
			Name:        "*",
			Description: "namespace reserved for " + ns,
		})
	}
	c.mu.RLock()
	for _, ns := range c.pluginNamespaces {
		out = append(out, SlashCommandInfo{
			Namespace:   ns,
			Name:        "*",
			Description: "plugin-claimed namespace",
		})
	}
	c.mu.RUnlock()
	return out
}

// ListSkills returns the loaded skill tier-1 catalog.
func (c *Catalog) ListSkills() []skillmd.Tier1 {
	if c.Skills == nil {
		return nil
	}
	return c.Skills()
}

// Handshake returns the protocol handshake. Provided here so REST
// and mcpserver share one implementation.
func (c *Catalog) Handshake(features []string) control.Handshake {
	return control.CurrentHandshake(features)
}

// countAssistant counts assistant-role messages in an engine's history.
// We re-derive rather than tracking turns in another place.
func countAssistant(e *engine.Engine) int {
	n := 0
	for _, m := range e.History {
		if m.Role == provider.RoleAssistant {
			n++
		}
	}
	return n
}
