package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func listJSON(t *testing.T, s *Server, method string) string {
	t.Helper()
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method})
	if resp.Error != nil {
		t.Fatalf("%s: %v", method, resp.Error)
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Icons set at registration appear on every listing, as 2025-11-25 added them to tools, resources, templates and prompts.
func TestIconsAppearInListings(t *testing.T) {
	icon := Icon{Src: "https://example.com/i.png", MimeType: "image/png", Sizes: []string{"48x48"}, Theme: "dark"}
	const want = `"icons":[{"src":"https://example.com/i.png","mimeType":"image/png","sizes":["48x48"],"theme":"dark"}]`
	s := NewServer()
	noop := func(context.Context, map[string]any) (any, error) { return "ok", nil }
	if err := s.RegisterTool("t", "d", map[string]any{"type": "object"}, noop, WithToolIcons(icon)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResource("r://x", "x", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "x"}, nil
	}, WithResourceIcons(icon)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResourceTemplate("r://{id}", "rt", "text/plain", WithResourceTemplateIcons(icon)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterPrompt("p", func(context.Context, map[string]string) ([]PromptMessage, error) {
		return nil, nil
	}, WithPromptIcons(icon)); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"tools/list", "resources/list", "resources/templates/list", "prompts/list"} {
		if got := listJSON(t, s, m); !strings.Contains(got, want) {
			t.Errorf("%s = %s, want it to carry %s", m, got, want)
		}
	}
	if got := listJSON(t, NewServer(), "tools/list"); strings.Contains(got, "icons") {
		t.Errorf("tools/list without icons = %s, want no icons key", got)
	}
}

// An icon option panics at registration on a src that is not https or data:, or a theme that is not light or dark.
func TestIconOptionsRefuseUnsafeSrc(t *testing.T) {
	for _, ic := range []Icon{
		{Src: "http://example.com/i.png"},
		{Src: "javascript:alert(1)"},
		{Src: "file:///etc/passwd"},
		{Src: "//example.com/i.png"},
		{Src: "data:image/png;base64"},
		{Src: "https://example.com/i.png", Theme: "sepia"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("WithToolIcons(%+v) did not panic", ic)
				}
			}()
			WithToolIcons(ic)
		}()
	}
	WithToolIcons(Icon{Src: "data:image/svg+xml;base64,PHN2Zy8+"}, Icon{Src: "https://example.com/i.svg", Theme: "light"})
}

// SetServerDetails fills serverInfo's optional fields on initialize and refuses, changing nothing, an unsafe URL.
func TestServerDetailsInInitialize(t *testing.T) {
	s := NewServer()
	s.SetServerInfo("acme", "2.0.0")
	if err := s.SetServerDetails(ServerDetails{WebsiteURL: "http://acme.example"}); err == nil {
		t.Fatal("http websiteUrl accepted")
	}
	if err := s.SetServerDetails(ServerDetails{Icons: []Icon{{Src: "ftp://x/y"}}}); err == nil {
		t.Fatal("ftp icon accepted")
	}
	if got := listJSON(t, s, "initialize"); strings.Contains(got, "websiteUrl") || strings.Contains(got, "icons") {
		t.Fatalf("a refused SetServerDetails changed serverInfo: %s", got)
	}
	if err := s.SetServerDetails(ServerDetails{
		Title: "Acme", Description: "Orders and refunds", WebsiteURL: "https://acme.example",
		Icons: []Icon{{Src: "https://acme.example/i.png"}},
	}); err != nil {
		t.Fatal(err)
	}
	got := listJSON(t, s, "initialize")
	for _, want := range []string{`"name":"acme"`, `"version":"2.0.0"`, `"title":"Acme"`, `"description":"Orders and refunds"`, `"websiteUrl":"https://acme.example"`, `"icons":[{"src":"https://acme.example/i.png"}]`} {
		if !strings.Contains(got, want) {
			t.Errorf("initialize = %s, want %s", got, want)
		}
	}
}

// Arguments failing the inputSchema answer tools/call as a tool execution error the model can correct (SEP-1303), while
// CallTool still returns the invalid-params RPCError to Go callers.
func TestArgsRefusalIsToolExecutionError(t *testing.T) {
	s := NewServer()
	ran := false
	schema := map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}}, "required": []string{"n"}}
	if err := s.RegisterTool("t", "d", schema, func(context.Context, map[string]any) (any, error) { ran = true; return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: json.RawMessage(`{"name":"t","arguments":{"n":"x"}}`)})
	b, _ := json.Marshal(resp)
	if resp.Error != nil || !strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), "arguments.n") || ran {
		t.Fatalf("tools/call = %s ran=%v, want an isError result naming arguments.n and no handler run", b, ran)
	}
	_, err := s.CallTool(context.Background(), "t", map[string]any{"n": "x"})
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != ErrInvalidParams || ran {
		t.Fatalf("CallTool err = %v ran=%v, want invalid params and no handler run", err, ran)
	}
}

// An unknown tool is invalid params and an unknown resource is -32002, as the 2025-11-25 tools and resources pages name them.
func TestNotFoundCodes(t *testing.T) {
	s := NewServer()
	tool := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: json.RawMessage(`{"name":"nope"}`)})
	if tool.Error == nil || tool.Error.Code != ErrInvalidParams {
		t.Errorf("unknown tool = %+v, want %d", tool.Error, ErrInvalidParams)
	}
	res := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: json.RawMessage(`{"uri":"r://nope"}`)})
	if res.Error == nil || res.Error.Code != ErrResourceNotFound {
		t.Errorf("unknown resource = %+v, want %d", res.Error, ErrResourceNotFound)
	}
}
