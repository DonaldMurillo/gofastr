package framework

import (
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// The introspection tool set is the AI-first front door: agents learn it
// from guidance, not from tools/list, so every surface that teaches it
// must name the full set. This pins guidance to the live registration —
// adding a tool to registerIntrospectionTools without updating the
// guidance fails here (the "five tools" drift already happened once).
func TestIntrospectionGuidanceNamesEveryTool(t *testing.T) {
	app := NewApp(WithMCPIntrospection(), WithMCPControl())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	tools := app.MCP.ListTools()
	if len(tools) == 0 {
		t.Fatal("no introspection tools registered")
	}

	surfaces := map[string]string{
		"framework/agents.md":                       "agents.md",
		".claude/skills/app-introspect/SKILL.md":    "../.claude/skills/app-introspect/SKILL.md",
		".claude/skills/gofastr-mcp-debug/SKILL.md": "../.claude/skills/gofastr-mcp-debug/SKILL.md",
	}
	bodies := make(map[string]string, len(surfaces)+1)
	for label, path := range surfaces {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", label, err)
		}
		bodies[label] = string(b)
	}
	agentReady, err := docs.Get("agent-ready")
	if err != nil {
		t.Fatalf("docs.Get(agent-ready): %v", err)
	}
	bodies["docs/content/agent-ready.md"] = string(agentReady)

	for _, tool := range tools {
		for label, body := range bodies {
			if !strings.Contains(body, tool.Name) {
				t.Errorf("%s never names introspection tool %q — agents reading it won't know the tool exists", label, tool.Name)
			}
		}
	}
}
