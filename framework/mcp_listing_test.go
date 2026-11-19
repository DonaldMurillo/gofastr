package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

func TestWithMCPInstructionsReachesInitialize(t *testing.T) {
	custom := mcp.NewServer()
	app := NewApp(WithMCPInstructions("Generate barcodes."), WithMCPServer(custom))
	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize: %v", resp.Error)
	}
	if got := resp.Result.(map[string]any)["instructions"]; got != "Generate barcodes." {
		t.Fatalf("instructions = %#v, want the WithMCPInstructions text on the server WithMCPServer installed", got)
	}
}

func TestOpenAIAppsChallengeServed(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithOpenAIAppsChallenge("  tok_AbC-123.xyz\n")))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "tok_AbC-123.xyz" {
		t.Fatalf("status %d body %q, want 200 and the trimmed token", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q, want text/plain", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("challenge answer is missing nosniff")
	}

	bare, cleanupBare := startApp(t, NewApp(WithOpenAIAppsChallenge("")))
	defer cleanupBare()
	rec = httptest.NewRecorder()
	bare.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openai-apps-challenge", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty token: status %d, want 404", rec.Code)
	}
}

func TestOpenAIAppsChallengeRefusesBadToken(t *testing.T) {
	for name, token := range map[string]string{
		"newline":   "Zq9\nX-Injected: 1",
		"carriage":  "Zq9\rabc",
		"nul":       "Zq9\x00abc",
		"space":     "Zq9 abc",
		"non-ascii": "Zq9\u202eabc",
		"del":       "Zq9\x7fabc",
		"too long":  "Zq9" + strings.Repeat("a", maxOpenAIAppsChallengeBytes),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				rec := recover()
				if rec == nil {
					t.Fatal("WithOpenAIAppsChallenge accepted the token")
				}
				if msg, _ := rec.(string); strings.Contains(msg, "Zq9") {
					t.Errorf("panic message quotes the token: %q", msg)
				}
			}()
			WithOpenAIAppsChallenge(token)
		})
	}
}

func TestDevModeWarnsOnUnannotatedTools(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)
	noop := func(context.Context, map[string]any) (any, error) { return nil, nil }

	t.Setenv("GOFASTR_DEV", "")
	if err := NewApp().MCP.RegisterTool("bare_prod", "", nil, noop); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "" {
		t.Fatalf("warned outside dev mode: %#v", sink.Attrs())
	}

	t.Setenv("GOFASTR_DEV", "1")
	if err := NewApp().MCP.RegisterTool("bare_dev", "", nil, noop); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "bare_dev" || !strings.Contains(sink.Get("missing"), "readOnlyHint") {
		t.Fatalf("dev mode did not warn about an unannotated tool: %#v", sink.Attrs())
	}
}

func TestBuiltinMCPToolsCarryListingHints(t *testing.T) {
	app := NewApp(WithMCPIntrospection(), WithMCPControl())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	tools := app.MCP.ListTools()
	if len(tools) == 0 {
		t.Fatal("setup: no built-in tools registered")
	}
	for _, tool := range tools {
		if a := tool.Annotations; a == nil || a.ReadOnlyHint == nil || a.DestructiveHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: annotations %+v, want readOnlyHint, destructiveHint and openWorldHint set", tool.Name, a)
		}
	}

	ent := entity.Define("orders", entity.EntityConfig{Name: "orders", Fields: []schema.Field{{Name: "n", Type: schema.String}}})
	ep := entity.Endpoint{Method: http.MethodGet, Path: "summary", MCPAnnotations: &mcp.Annotations{ReadOnlyHint: mcp.Bool(true)}}
	var tool mcp.Tool
	for _, opt := range endpointMCPOptions(ent, ep) {
		opt(&tool)
	}
	if tool.Annotations == nil || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("endpoint MCPAnnotations did not reach the tool: %+v", tool.Annotations)
	}
}
