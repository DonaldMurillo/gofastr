package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ToolSource produces tools to register with the engine. Built-ins,
// MCP-bridged tools, and plugin-contributed tools all implement this.
type ToolSource interface {
	// Name identifies the source for logging + the catalog ("builtin",
	// "mcp:kiln", "plugin:foo").
	Name() string

	// Tools returns the current snapshot of tools this source
	// exposes. Sources that hot-reload (MCP server reconnects, skill
	// changes) should re-call Registry.Replace on change.
	Tools(ctx context.Context) ([]Tool, error)
}

// Registry is the in-memory catalog of tools available to the
// engine. Multiple ToolSources can register; their tools are merged
// by name, with later registrations rejected on collision (to make
// shadowing explicit rather than silent).
type Registry struct {
	mu      sync.RWMutex
	sources map[string]ToolSource
	tools   map[string]toolEntry // by name
}

type toolEntry struct {
	tool   Tool
	source string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		sources: make(map[string]ToolSource),
		tools:   make(map[string]toolEntry),
	}
}

// Register adds a ToolSource and ingests its current tool set.
// Returns an error on duplicate source name or duplicate tool name
// across sources.
func (r *Registry) Register(ctx context.Context, src ToolSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[src.Name()]; exists {
		return fmt.Errorf("tool: source %q already registered", src.Name())
	}
	tools, err := src.Tools(ctx)
	if err != nil {
		return fmt.Errorf("tool: source %q Tools(): %w", src.Name(), err)
	}
	for _, t := range tools {
		if existing, dup := r.tools[t.Name()]; dup {
			return fmt.Errorf("tool: name %q exposed by both %q and %q",
				t.Name(), existing.source, src.Name())
		}
	}
	r.sources[src.Name()] = src
	for _, t := range tools {
		r.tools[t.Name()] = toolEntry{tool: t, source: src.Name()}
	}
	return nil
}

// Replace updates the tool set for a single source (e.g., MCP server
// reconnect with a different tool list). Tools no longer exposed by
// the source are removed; new ones are added; collisions with other
// sources return an error and leave the registry unchanged.
func (r *Registry) Replace(ctx context.Context, sourceName string, tools []Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[sourceName]; !exists {
		return fmt.Errorf("tool: source %q not registered", sourceName)
	}
	// Detect collisions with other sources.
	for _, t := range tools {
		if existing, dup := r.tools[t.Name()]; dup && existing.source != sourceName {
			return fmt.Errorf("tool: name %q collides with source %q",
				t.Name(), existing.source)
		}
	}
	// Remove old tools from this source.
	for name, e := range r.tools {
		if e.source == sourceName {
			delete(r.tools, name)
		}
	}
	// Add new ones.
	for _, t := range tools {
		r.tools[t.Name()] = toolEntry{tool: t, source: sourceName}
	}
	return nil
}

// Lookup returns the tool with the given name, or an error if absent.
func (r *Registry) Lookup(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool: %q not registered", name)
	}
	return e.tool, nil
}

// List returns all tools, sorted by name. Callers should treat the
// result as a snapshot; the registry may change after this returns.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, e := range r.tools {
		out = append(out, e.tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// SourceOf returns the name of the source that registered the given
// tool, or "" if the tool is not registered.
func (r *Registry) SourceOf(toolName string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[toolName]
	if !ok {
		return ""
	}
	return e.source
}

// ErrToolUnknown is returned by Lookup when the tool is not registered.
var ErrToolUnknown = errors.New("tool: unknown")
