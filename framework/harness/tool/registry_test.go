package tool

import (
	"context"
	"strings"
	"testing"
)

type fakeTool struct {
	name string
}

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake " + f.name }
func (f fakeTool) InputSchema() []byte { return []byte(`{}`) }
func (f fakeTool) Mutating() bool      { return false }
func (f fakeTool) Run(ctx context.Context, c ToolCall, sink EventSink) (*ToolResult, error) {
	return &ToolResult{}, nil
}

type staticSource struct {
	name  string
	tools []Tool
}

func (s staticSource) Name() string                                { return s.name }
func (s staticSource) Tools(ctx context.Context) ([]Tool, error)   { return s.tools, nil }

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	src := staticSource{name: "builtin", tools: []Tool{fakeTool{name: "Read"}, fakeTool{name: "Write"}}}
	if err := r.Register(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup("Read"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup("Nope"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
	got := r.List()
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	if got[0].Name() != "Read" || got[1].Name() != "Write" {
		t.Errorf("sort order off: %s, %s", got[0].Name(), got[1].Name())
	}
	if r.SourceOf("Read") != "builtin" {
		t.Errorf("SourceOf = %q, want builtin", r.SourceOf("Read"))
	}
}

func TestRegistryDuplicateSourceRejected(t *testing.T) {
	r := NewRegistry()
	src := staticSource{name: "builtin", tools: []Tool{fakeTool{name: "Read"}}}
	_ = r.Register(context.Background(), src)
	if err := r.Register(context.Background(), src); err == nil {
		t.Fatal("expected duplicate source error")
	}
}

func TestRegistryNameCollisionAcrossSources(t *testing.T) {
	r := NewRegistry()
	a := staticSource{name: "a", tools: []Tool{fakeTool{name: "X"}}}
	b := staticSource{name: "b", tools: []Tool{fakeTool{name: "X"}}}
	if err := r.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	err := r.Register(context.Background(), b)
	if err == nil || !strings.Contains(err.Error(), "X") {
		t.Fatalf("expected collision error mentioning X, got %v", err)
	}
}

func TestRegistryReplace(t *testing.T) {
	r := NewRegistry()
	src := staticSource{name: "mcp:kiln", tools: []Tool{fakeTool{name: "kiln.create_entity"}}}
	_ = r.Register(context.Background(), src)
	// MCP server reconnects with a different tool set.
	if err := r.Replace(context.Background(), "mcp:kiln",
		[]Tool{fakeTool{name: "kiln.create_entity_v2"}, fakeTool{name: "kiln.list"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup("kiln.create_entity"); err == nil {
		t.Error("old tool still present after Replace")
	}
	if _, err := r.Lookup("kiln.create_entity_v2"); err != nil {
		t.Error("new tool missing after Replace")
	}
}
