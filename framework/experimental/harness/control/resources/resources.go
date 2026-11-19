package resources

import (
	"context"
	"log/slog"
	"slices"
	"sort"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/skill/skillmd"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/slash"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Catalog is the read-only view of sessions, tools, providers, skills and commands the transports serve.
type Catalog struct {
	mu sync.RWMutex

	engines          map[ids.SessionID]*engine.Engine
	pluginNamespaces []string

	Tools     *tool.Registry
	Providers []provider.Provider
	// Skills returns the loaded skills' tier-1 metadata.
	Skills func() []skillmd.Tier1
}

// NewCatalog returns an empty Catalog; the caller sets Tools, Providers and Skills.
func NewCatalog() *Catalog {
	return &Catalog{
		engines: make(map[ids.SessionID]*engine.Engine),
	}
}

// RegisterEngine adds a session's engine to the session listing.
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

// SessionsAllowed keeps the sessions the claims may attach to.
func SessionsAllowed(list []SessionInfo, claims auth.Claims) []SessionInfo {
	return slices.DeleteFunc(list, func(si SessionInfo) bool { return !claims.AllowsSession(si.SessionID) })
}

// ListProviders returns each provider with its models; a provider whose listing fails is logged and shows none.
func (c *Catalog) ListProviders(ctx context.Context) []ProviderInfo {
	out := make([]ProviderInfo, 0, len(c.Providers))
	for _, p := range c.Providers {
		models, err := p.Models(ctx)
		if err != nil {
			slog.Warn("harness control: listing models", "provider", p.Name(), "error", err)
			models = nil
		}
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
	for _, b := range slash.AllBuiltins() {
		out = append(out, SlashCommandInfo{
			Name: b.Name, Description: b.Description, ArgsHelp: b.ArgsHelp, IsBuiltin: true,
		})
	}
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

// ListProfiles returns the names of the built-in profiles.
func (c *Catalog) ListProfiles() []string {
	return []string{"framework", "default"}
}

// ListSkills returns the loaded skill tier-1 catalog.
func (c *Catalog) ListSkills() []skillmd.Tier1 {
	if c.Skills == nil {
		return nil
	}
	return c.Skills()
}

// Handshake returns the protocol handshake advertising features.
func (c *Catalog) Handshake(features []string) control.Handshake {
	return control.CurrentHandshake(features)
}

func countAssistant(e *engine.Engine) int {
	n := 0
	for _, m := range e.History {
		if m.Role == provider.RoleAssistant {
			n++
		}
	}
	return n
}
