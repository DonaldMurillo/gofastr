package profile

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFrameworkPreset(t *testing.T) {
	p, err := Load(filepath.Join("framework.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "framework" {
		t.Errorf("name = %q", p.Name)
	}
	if p.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", p.SchemaVersion)
	}
	if p.DefaultModel != "openrouter:anthropic/claude-sonnet-4" {
		t.Errorf("default_model = %q", p.DefaultModel)
	}
	if !contains(p.ContextSources, "AGENTS.md") || !contains(p.ContextSources, "CLAUDE.md") {
		t.Errorf("context_sources = %v", p.ContextSources)
	}
	if len(p.MCPServers) != 1 || p.MCPServers[0].Name != "gofastr-introspection" {
		t.Errorf("mcp_servers = %+v", p.MCPServers)
	}
	if p.AllowProjectHooks {
		t.Error("allow_project_hooks default should be false")
	}
}

func TestLoadDefaultPreset(t *testing.T) {
	p, err := Load(filepath.Join("default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "default" {
		t.Errorf("name = %q", p.Name)
	}
	if !strings.HasPrefix(p.DefaultModel, "zai:") {
		t.Errorf("default_model = %q", p.DefaultModel)
	}
	if p.DefaultModel != "zai:glm-5.1" {
		t.Errorf("default model should be glm-5.1, got %q", p.DefaultModel)
	}
}

func TestParseRejectsUnsupportedSchemaVersion(t *testing.T) {
	src := `
schema_version = 99
name = "x"
default_model = "m"
`
	_, err := Parse(strings.NewReader(src))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	src := `
schema_version = 1
name = "x"
default_model = "m"
mystery_field = "nope"
`
	_, err := Parse(strings.NewReader(src))
	if err == nil || !strings.Contains(err.Error(), "mystery_field") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseTripleQuoted(t *testing.T) {
	src := `
schema_version = 1
name = "x"
default_model = "m"
prompt_header = """
multi
line
"""
`
	p, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.PromptHeader, "multi") || !strings.Contains(p.PromptHeader, "line") {
		t.Errorf("prompt_header = %q", p.PromptHeader)
	}
}

func TestParseMCPServersWithArgs(t *testing.T) {
	src := `
schema_version = 1
name = "x"
default_model = "m"
mcp_servers = [
  { name = "kiln", cmd = "kiln", args = ["mcp", "--debug"], discovery = "lazy" },
]
`
	p, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.MCPServers) != 1 {
		t.Fatalf("servers = %d", len(p.MCPServers))
	}
	got := p.MCPServers[0]
	if got.Discovery != "lazy" {
		t.Errorf("discovery = %q", got.Discovery)
	}
	if len(got.Args) != 2 || got.Args[0] != "mcp" || got.Args[1] != "--debug" {
		t.Errorf("args = %v", got.Args)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
