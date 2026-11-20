package tool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ToolSource is a named set of tools, such as "builtin" or "mcp:kiln"; a source whose set changes calls Registry.Replace.
type ToolSource interface {
	Name() string
	Tools(ctx context.Context) ([]Tool, error)
}

// Registry is the catalog of tools by name, merged from its sources; a name two sources expose is refused.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]ToolSource
	tools   map[string]toolEntry
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

// Register adds src and its tools, or errors and changes nothing when the source or a tool name is taken.
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

// Replace swaps a registered source's tools for tools, or errors and changes nothing when another source owns a name.
func (r *Registry) Replace(ctx context.Context, sourceName string, tools []Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[sourceName]; !exists {
		return fmt.Errorf("tool: source %q not registered", sourceName)
	}
	for _, t := range tools {
		if existing, dup := r.tools[t.Name()]; dup && existing.source != sourceName {
			return fmt.Errorf("tool: name %q collides with source %q",
				t.Name(), existing.source)
		}
	}
	for name, e := range r.tools {
		if e.source == sourceName {
			delete(r.tools, name)
		}
	}
	for _, t := range tools {
		r.tools[t.Name()] = toolEntry{tool: t, source: sourceName}
	}
	return nil
}

// Lookup returns the named tool, or an error wrapping ErrToolUnknown.
func (r *Registry) Lookup(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q not registered", ErrToolUnknown, name)
	}
	return e.tool, nil
}

// List returns a snapshot of every tool, sorted by name.
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

// SourceOf returns the name of the source that registered toolName, or "".
func (r *Registry) SourceOf(toolName string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[toolName]
	if !ok {
		return ""
	}
	return e.source
}

// ErrToolUnknown is wrapped by the error Lookup returns for an unregistered tool.
var ErrToolUnknown = errors.New("tool: unknown")
