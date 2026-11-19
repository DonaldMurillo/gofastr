package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// hintString renders a tool's four behaviour hints, "-" for an unset one.
func hintString(a *mcp.Annotations) string {
	if a == nil {
		return "none"
	}
	one := func(p *bool) string {
		if p == nil {
			return "-"
		}
		return strconv.FormatBool(*p)
	}
	return "readOnly=" + one(a.ReadOnlyHint) + " destructive=" + one(a.DestructiveHint) +
		" idempotent=" + one(a.IdempotentHint) + " openWorld=" + one(a.OpenWorldHint)
}

func TestBuiltinMCPToolsCarryListingHints(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_DEV_MCP", "")
	t.Setenv("GOFASTR_ENV", "")
	const (
		read   = "readOnly=true destructive=false idempotent=true openWorld=false"
		toggle = "readOnly=false destructive=false idempotent=true openWorld=false"
		write  = "readOnly=false destructive=true idempotent=false openWorld=false"
	)
	want := map[string]string{
		"app_routes": read, "app_plugins": read, "app_batteries": read, "app_modules": read,
		"app_config": read, "app_readiness": read, "app_goroutine_leaks": read,
		"app_routines": read, "app_unmounted": read,
		"contracts_list": read, "contracts_explain": read, "contracts_capabilities": read,
		"contracts_verify": read, "contracts_fix": write,
		"app_module_enable": toggle, "app_module_disable": toggle,
	}
	app := NewApp(WithMCPIntrospection(), WithMCPControl())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	got := map[string]string{}
	for _, tool := range app.MCP.ListTools() {
		got[tool.Name] = hintString(tool.Annotations)
	}
	for name, hints := range want {
		if got[name] != hints {
			t.Errorf("%s: hints %q, want %q", name, got[name], hints)
		}
	}
	for name := range got {
		if _, listed := want[name]; !listed {
			t.Errorf("built-in tool %s is not in this test's table; add its expected hints", name)
		}
	}

}

func TestEndpointMCPAnnotationsServed(t *testing.T) {
	endpoint := func(name string) []entity.Endpoint {
		return []entity.Endpoint{{
			Method: http.MethodGet, Path: "summary", Name: name, MCP: true, Public: true,
			MCPHandler: func(context.Context, map[string]any) (any, error) { return "ok", nil },
			MCPAnnotations: &mcp.Annotations{
				ReadOnlyHint: mcp.Bool(true), DestructiveHint: mcp.Bool(false), OpenWorldHint: mcp.Bool(true),
			},
		}}
	}
	noCRUD := &entity.ExposureConfig{CRUD: new(false)}
	fields := []schema.Field{{Name: "n", Type: schema.String}}
	app := NewApp()
	app.Entity("orders", entity.EntityConfig{Fields: fields, Exposure: noCRUD, Endpoints: endpoint("orders_summary")})
	app.GroupEntity(app.Group("/v2"), "invoices", entity.EntityConfig{Fields: fields, Exposure: noCRUD, Endpoints: endpoint("invoices_summary")})

	resp := app.MCP.HandleRequest(context.Background(), mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list: %v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Annotations json.RawMessage `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tool := range listed.Tools {
		got[tool.Name] = string(tool.Annotations)
	}
	const want = `{"readOnlyHint":true,"destructiveHint":false,"openWorldHint":true}`
	for _, name := range []string{"orders_summary", "invoices_summary"} {
		if got[name] != want {
			t.Errorf("%s: tools/list annotations %s, want %s", name, got[name], want)
		}
	}
}

func TestDevWarningsCoverToolsOnACustomServer(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)
	t.Setenv("GOFASTR_DEV", "1")
	custom := mcp.NewServer()
	if err := custom.RegisterTool("early_bare", "", nil, func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	NewApp(WithMCPServer(custom))
	if sink.Get("tool") != "early_bare" {
		t.Fatalf("a tool registered on the custom server before NewApp was not warned about: %#v", sink.Attrs())
	}
}
