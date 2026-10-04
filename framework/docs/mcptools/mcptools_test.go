package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/docs"
)

func docsApp(t *testing.T, opts ...framework.AppOption) *framework.App {
	t.Helper()
	app := framework.NewApp(append([]framework.AppOption{framework.WithMCPTools(Register)}, opts...)...)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	return app
}

func toolNames(app *framework.App) map[string]bool {
	got := map[string]bool{}
	for _, tool := range app.MCP.ListTools() {
		got[tool.Name] = true
	}
	return got
}

// TestRegisterInstallsThreeTools pins the opt-in: the registrar alone
// installs the docs tools, with or without introspection.
func TestRegisterInstallsThreeTools(t *testing.T) {
	got := toolNames(docsApp(t))
	for _, want := range []string{"framework_docs_list", "framework_docs_get", "framework_docs_search"} {
		if !got[want] {
			t.Errorf("docs tool %q was not registered", want)
		}
	}
}

// TestIntrospectionAloneRegistersNoDocsTools pins the broken edge:
// WithMCPIntrospection no longer carries the docs tools, so framework
// no longer needs the embedded corpus.
func TestIntrospectionAloneRegistersNoDocsTools(t *testing.T) {
	app := framework.NewApp(framework.WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	for name := range toolNames(app) {
		if strings.HasPrefix(name, "framework_docs_") {
			t.Errorf("WithMCPIntrospection registered %q; the docs tools belong to mcptools.Register", name)
		}
	}
}

// TestFrameworkDoesNotDependOnDocs is the gate the package exists for:
// the framework package's dependency closure must not contain
// framework/docs, or a docs edit widens the affected set to the tree.
func TestFrameworkDoesNotDependOnDocs(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-f", `{{join .Deps "\n"}}`, "github.com/DonaldMurillo/gofastr/framework")
	cmd.Dir = "../../.."
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Split(string(out), "\n") {
		if dep == "github.com/DonaldMurillo/gofastr/framework/docs" {
			t.Fatal("framework imports framework/docs again; every docs edit would run the whole suite (#470)")
		}
	}
}

// TestFrameworkDocsListReturnsTopics verifies framework_docs_list
// surfaces the embedded markdown tree.
func TestFrameworkDocsListReturnsTopics(t *testing.T) {
	app := docsApp(t)
	result, err := app.MCP.CallTool(context.Background(), "framework_docs_list", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool framework_docs_list: %v", err)
	}
	m := result.(map[string]any)
	if count := m["count"].(int); count == 0 {
		t.Fatal("framework_docs_list returned 0 topics — embed broken?")
	}
}

// TestFrameworkDocsCapabilityMapDiscovery pins parity between the
// embedded docs, task-oriented search, and the live app's MCP surface.
func TestFrameworkDocsCapabilityMapDiscovery(t *testing.T) {
	app := docsApp(t)
	result, err := app.MCP.CallTool(context.Background(), "framework_docs_get",
		map[string]any{"topic": "ui-capability-map"})
	if err != nil {
		t.Fatalf("framework_docs_get ui-capability-map: %v", err)
	}
	if markdown, _ := result.(map[string]any)["markdown"].(string); !strings.Contains(markdown, "Live dashboards") {
		t.Fatalf("MCP capability map is missing live-dashboard guidance")
	}

	result, err = app.MCP.CallTool(context.Background(), "framework_docs_search",
		map[string]any{"term": "live dashboard"})
	if err != nil {
		t.Fatalf("framework_docs_search: %v", err)
	}
	hits := result.(map[string]any)["hits"].([]map[string]any)
	found := false
	for _, hit := range hits {
		if hit["topic"] == "ui-capability-map" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("MCP search did not route live dashboard to ui-capability-map: %+v", hits)
	}
}

// TestFrameworkDocsGetReadsTopic exercises a known topic round-trip.
func TestFrameworkDocsGetReadsTopic(t *testing.T) {
	app := docsApp(t)
	list, err := app.MCP.CallTool(context.Background(), "framework_docs_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	topics := list.(map[string]any)["topics"].([]map[string]any)
	if len(topics) == 0 {
		t.Skip("no embedded topics")
	}
	name := topics[0]["name"].(string)

	result, err := app.MCP.CallTool(context.Background(), "framework_docs_get", map[string]any{"topic": name})
	if err != nil {
		t.Fatalf("CallTool framework_docs_get: %v", err)
	}
	m := result.(map[string]any)
	if md, _ := m["markdown"].(string); md == "" {
		t.Errorf("markdown body empty for topic %q", name)
	}
	if m["name"] != name {
		t.Errorf("name echo = %v, want %q", m["name"], name)
	}
}

// TestDocsGetNotFound: an unknown topic surfaces the not-found error.
func TestDocsGetNotFound(t *testing.T) {
	app := docsApp(t)
	if _, err := app.MCP.CallTool(context.Background(), "framework_docs_get",
		map[string]any{"topic": "does-not-exist-xyz"}); err == nil {
		t.Fatal("expected not-found error")
	}
}

// TestDocsSearchRespectsLimit: framework_docs_search returns hits and
// honours the int limit param in every JSON-decoded shape.
func TestDocsSearchRespectsLimit(t *testing.T) {
	app := docsApp(t)
	res, err := app.MCP.CallTool(context.Background(), "framework_docs_search",
		map[string]any{"term": "entity", "limit": float64(3)})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	m := res.(map[string]any)
	if m["term"] != "entity" {
		t.Fatalf("term echo wrong: %v", m["term"])
	}
	if hits := m["hits"].([]map[string]any); len(hits) != 3 {
		t.Fatalf("limit=3 returned %d hits", len(hits))
	}
	for _, limit := range []any{int(2), int64(2)} {
		res, err := toolDocsSearch(context.Background(), map[string]any{"term": "entity", "limit": limit})
		if err != nil {
			t.Fatalf("%T limit: %v", limit, err)
		}
		if got := res.(map[string]any)["count"].(int); got != 2 {
			t.Fatalf("%T limit=2 returned %d hits", limit, got)
		}
	}
}

// maxDocsSearchResponseBytes bounds the marshaled tool reply.
// maxDocsSearchHits hits with 240-char excerpts plus per-hit metadata
// stays comfortably under 256 KiB; an uncapped scan of the ~1.5 MB
// embedded corpus for a stopword term marshals to multiple megabytes.
const maxDocsSearchResponseBytes = 256 << 10

// TestDocsSearchLimitIsCapped pins the oversized-response protection
// of framework_docs_search: a caller-supplied numeric limit can never
// push the reply past the documented hard cap. The tool exists to be
// called by agents with narrow contexts.
func TestDocsSearchLimitIsCapped(t *testing.T) {
	docsApp(t)
	call := func(t *testing.T, params map[string]any) map[string]any {
		t.Helper()
		res, err := toolDocsSearch(context.Background(), params)
		if err != nil {
			t.Fatalf("toolDocsSearch(%v): %v", params, err)
		}
		return res.(map[string]any)
	}
	// The three branches of the handler's limit type switch must all
	// clamp. float64 is the realistic MCP wire form.
	for _, tc := range []struct {
		name  string
		limit any
	}{
		{"float64 limit 1e12", 1e12},
		{"int limit 1<<30", 1 << 30},
		{"int64 limit 1<<40", int64(1) << 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, map[string]any{"term": "the", "limit": tc.limit})
			if count := res["count"].(int); count > maxDocsSearchHits {
				t.Fatalf("framework_docs_search returned %d hits for limit=%v; the schema promises at most %d", count, tc.limit, maxDocsSearchHits)
			}
			b, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal reply: %v", err)
			}
			if len(b) > maxDocsSearchResponseBytes {
				t.Fatalf("reply marshals to %d bytes for limit=%v; over the %d-byte budget", len(b), tc.limit, maxDocsSearchResponseBytes)
			}
		})
	}
	t.Run("small limit still honored", func(t *testing.T) {
		if got := call(t, map[string]any{"term": "the", "limit": 5})["count"].(int); got != 5 {
			t.Fatalf("limit=5 returned %d hits, want exactly 5", got)
		}
	})
	t.Run("omitted limit uses default cap", func(t *testing.T) {
		if got := call(t, map[string]any{"term": "the"})["count"].(int); got > 50 {
			t.Fatalf("omitted limit returned %d hits, want <= 50 (defaultSearchHitCap)", got)
		}
	})
}

// TestGuidanceNamesEveryTool: the introspection and docs tool set is
// the AI-first front door; agents learn it from guidance, not from
// tools/list, so every surface that teaches it must name the full set.
// Registering a tool without updating the guidance fails here (the
// "five tools" drift already happened once). GOFASTR_DEV implies the
// dev-only tools, which an agent meets first.
func TestGuidanceNamesEveryTool(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	app := docsApp(t, framework.WithMCPIntrospection(), framework.WithMCPControl())
	tools := app.MCP.ListTools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	surfaces := map[string]string{
		"framework/agents.md":                       "../../agents.md",
		".claude/skills/app-introspect/SKILL.md":    "../../../.claude/skills/app-introspect/SKILL.md",
		".claude/skills/gofastr-mcp-debug/SKILL.md": "../../../.claude/skills/gofastr-mcp-debug/SKILL.md",
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
				t.Errorf("%s never names tool %q — agents reading it won't know the tool exists", label, tool.Name)
			}
		}
	}
}
