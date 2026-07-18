package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
)

// wireResult marshals a Response's Result to a generic map, exercising the
// custom JSON marshaling (Content.MarshalJSON etc.) exactly as a client sees it.
func wireResult(t *testing.T, resp Response) map[string]any {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %v", resp.Error)
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return m
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) Response {
	t.Helper()
	p, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	return s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
}

// ── Part 1: rich tool result content ──────────────────────────────

func TestImageResult_EmitsImageBlock(t *testing.T) {
	s := NewServer()
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	_ = s.RegisterTool("gen", "", nil, func(context.Context, map[string]any) (any, error) {
		return ImageResult{Data: png, MimeType: "image/png"}, nil
	})

	m := wireResult(t, callTool(t, s, "gen", nil))
	content := m["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("want 1 block, got %d", len(content))
	}
	block := content[0].(map[string]any)
	if block["type"] != "image" {
		t.Errorf("type = %v, want image", block["type"])
	}
	if block["mimeType"] != "image/png" {
		t.Errorf("mimeType = %v", block["mimeType"])
	}
	if block["data"] != base64.StdEncoding.EncodeToString(png) {
		t.Errorf("data not base64 of png bytes")
	}
	// An image block must NOT carry a stray empty text field.
	if _, ok := block["text"]; ok {
		t.Errorf("image block leaked a text field: %v", block)
	}
}

func TestResult_StructuredContentAndTextMirror(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("stat", "", nil, func(context.Context, map[string]any) (any, error) {
		return ToolResult{Structured: map[string]any{"count": 3}}, nil
	})

	m := wireResult(t, callTool(t, s, "stat", nil))
	sc, ok := m["structuredContent"].(map[string]any)
	if !ok || sc["count"].(float64) != 3 {
		t.Fatalf("structuredContent = %v, want {count:3}", m["structuredContent"])
	}
	// Structured-only results still mirror a text block for plain clients.
	content := m["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("want a mirrored text block, got %v", content)
	}
}

func TestResult_ExplicitBlocks(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("multi", "", nil, func(context.Context, map[string]any) (any, error) {
		return ToolResult{Content: []Content{TextContent("hi"), ImageContent([]byte{1, 2}, "image/gif")}}, nil
	})
	m := wireResult(t, callTool(t, s, "multi", nil))
	content := m["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(content))
	}
	if content[0].(map[string]any)["text"] != "hi" {
		t.Errorf("block 0 text = %v", content[0])
	}
	if content[1].(map[string]any)["type"] != "image" {
		t.Errorf("block 1 type = %v", content[1])
	}
}

func TestPlainValueStillText(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("legacy", "", nil, func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	m := wireResult(t, callTool(t, s, "legacy", nil))
	content := m["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Fatalf("legacy plain value should stay a text block, got %v", block)
	}
	if block["text"] != `{"ok":true}` {
		t.Errorf("text = %v, want JSON-marshaled value", block["text"])
	}
}

func TestOutputSchema_SerializedInList(t *testing.T) {
	s := NewServer()
	schema := map[string]any{"type": "object"}
	_ = s.RegisterTool("typed", "", nil,
		func(context.Context, map[string]any) (any, error) { return nil, nil },
		WithOutputSchema(schema))

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	m := wireResult(t, resp)
	tool := m["tools"].([]any)[0].(map[string]any)
	if tool["outputSchema"] == nil {
		t.Fatalf("outputSchema not serialized: %v", tool)
	}
}

// ── Part 3: tool _meta passthrough ────────────────────────────────

func TestWithMeta_SerializedInList(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("widget", "", nil,
		func(context.Context, map[string]any) (any, error) { return nil, nil },
		WithToolMeta(map[string]any{"ui": map[string]any{"resourceUri": "ui://app/widget.html"}}))

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	m := wireResult(t, resp)
	tool := m["tools"].([]any)[0].(map[string]any)
	meta, ok := tool["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta not serialized: %v", tool)
	}
	ui := meta["ui"].(map[string]any)
	if ui["resourceUri"] != "ui://app/widget.html" {
		t.Errorf("resourceUri = %v", ui["resourceUri"])
	}
}

func TestNoMeta_OmitsField(t *testing.T) {
	s := NewServer()
	_ = s.RegisterTool("plain", "", nil, func(context.Context, map[string]any) (any, error) { return nil, nil })
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	m := wireResult(t, resp)
	tool := m["tools"].([]any)[0].(map[string]any)
	if _, ok := tool["_meta"]; ok {
		t.Errorf("tool without meta leaked _meta: %v", tool)
	}
}

// ── Part 2: resources capability ──────────────────────────────────

func staticResource(s *Server, uri, name, mime, body string, opts ...ResourceOption) error {
	return s.RegisterResource(uri, name, mime, func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: body}, nil
	}, opts...)
}

func TestResourcesList_ReturnsRegistered(t *testing.T) {
	s := NewServer()
	if err := staticResource(s, "ui://app/x.html", "X", "text/html", "<h1>x</h1>",
		WithResourceDescription("the x widget"),
		WithResourceMeta(map[string]any{"ui": map[string]any{"csp": "default-src 'self'"}})); err != nil {
		t.Fatal(err)
	}
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/list"})
	m := wireResult(t, resp)
	res := m["resources"].([]any)
	if len(res) != 1 {
		t.Fatalf("want 1 resource, got %d", len(res))
	}
	r := res[0].(map[string]any)
	if r["uri"] != "ui://app/x.html" || r["name"] != "X" || r["mimeType"] != "text/html" {
		t.Errorf("resource fields wrong: %v", r)
	}
	if r["description"] != "the x widget" {
		t.Errorf("description = %v", r["description"])
	}
	if r["_meta"] == nil {
		t.Errorf("resource _meta not serialized: %v", r)
	}
}

func TestResourcesRead_Text(t *testing.T) {
	s := NewServer()
	_ = staticResource(s, "ui://app/x.html", "X", "text/html", "<h1>x</h1>")
	p, _ := json.Marshal(map[string]any{"uri": "ui://app/x.html"})
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	m := wireResult(t, resp)
	c := m["contents"].([]any)[0].(map[string]any)
	if c["uri"] != "ui://app/x.html" || c["text"] != "<h1>x</h1>" || c["mimeType"] != "text/html" {
		t.Errorf("read contents wrong: %v", c)
	}
	if _, ok := c["blob"]; ok {
		t.Errorf("text resource leaked a blob field: %v", c)
	}
}

func TestResourcesRead_Blob(t *testing.T) {
	s := NewServer()
	raw := []byte{0x00, 0x01, 0x02, 0xff}
	_ = s.RegisterResource("blob://b", "B", "application/octet-stream", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Blob: raw}, nil
	})
	p, _ := json.Marshal(map[string]any{"uri": "blob://b"})
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	m := wireResult(t, resp)
	c := m["contents"].([]any)[0].(map[string]any)
	if c["blob"] != base64.StdEncoding.EncodeToString(raw) {
		t.Errorf("blob not base64: %v", c["blob"])
	}
	if _, ok := c["text"]; ok {
		t.Errorf("blob resource leaked a text field: %v", c)
	}
}

func TestResourcesRead_ContentsFuncSeesContext(t *testing.T) {
	// The contents func receives the request context, so a host can self-gate
	// sensitive resources by inspecting the caller (documented affordance —
	// resources are not covered by the tool call gate).
	s := NewServer()
	type ctxKey string
	const k ctxKey = "role"
	var seen any
	_ = s.RegisterResource("secret://x", "X", "text/plain", func(ctx context.Context) (ResourceContents, error) {
		seen = ctx.Value(k)
		return ResourceContents{Text: "ok"}, nil
	})
	ctx := context.WithValue(context.Background(), k, "admin")
	p, _ := json.Marshal(map[string]any{"uri": "secret://x"})
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if seen != "admin" {
		t.Errorf("contents func did not see request context value: got %v", seen)
	}
}

func TestResourcesRead_Unknown(t *testing.T) {
	s := NewServer()
	p, _ := json.Marshal(map[string]any{"uri": "ui://nope"})
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	if resp.Error == nil || resp.Error.Code != ErrMethodNotFound {
		t.Fatalf("unknown uri should be not-found, got %v", resp.Error)
	}
}

func TestResourcesRead_PanicRecovers(t *testing.T) {
	s := NewServer()
	_ = s.RegisterResource("boom://x", "B", "text/plain", func(context.Context) (ResourceContents, error) {
		panic("kaboom")
	})
	p, _ := json.Marshal(map[string]any{"uri": "boom://x"})
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	if resp.Error == nil || resp.Error.Code != ErrInternalError {
		t.Fatalf("panic should become internal error, got %v", resp.Error)
	}
	if resp.Error.Message == "kaboom" {
		t.Errorf("panic value leaked to caller")
	}
}

func TestResourcesCapability_OnlyWhenRegistered(t *testing.T) {
	// No resources → no resources capability.
	s := NewServer()
	m := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"}))
	caps := m["capabilities"].(map[string]any)
	if _, ok := caps["resources"]; ok {
		t.Errorf("tools-only server advertised resources: %v", caps)
	}

	// With a resource → capability present.
	_ = staticResource(s, "ui://a", "A", "text/html", "x")
	m2 := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 2, Method: "initialize"}))
	caps2 := m2["capabilities"].(map[string]any)
	if _, ok := caps2["resources"]; !ok {
		t.Errorf("server with a resource did not advertise resources: %v", caps2)
	}
}

// ── Part 4: RegisterApp sugar ─────────────────────────────────────

func sampleApp() AppConfig {
	return AppConfig{
		Name:        "studio",
		Description: "open the studio",
		InputSchema: map[string]any{"type": "object"},
		Handler:     func(context.Context, map[string]any) (any, error) { return "ok", nil },
		ResourceURI: "ui://app/studio.html",
		HTML:        "<h1>studio</h1>",
		CSP:         "default-src 'self'",
	}
}

func TestRegisterApp_WiresToolAndResource(t *testing.T) {
	s := NewServer()
	if err := s.RegisterApp(sampleApp()); err != nil {
		t.Fatal(err)
	}

	// Tool carries the ui.resourceUri linkage + the ChatGPT compat alias.
	lm := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}))
	tool := lm["tools"].([]any)[0].(map[string]any)
	meta := tool["_meta"].(map[string]any)
	if meta["ui"].(map[string]any)["resourceUri"] != "ui://app/studio.html" {
		t.Errorf("tool ui.resourceUri wrong: %v", meta)
	}
	if meta["openai/outputTemplate"] != "ui://app/studio.html" {
		t.Errorf("missing ChatGPT compat alias: %v", meta)
	}

	// Resource is served with the MCP App mime + _meta.ui.csp.
	rm := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 2, Method: "resources/list"}))
	res := rm["resources"].([]any)[0].(map[string]any)
	if res["uri"] != "ui://app/studio.html" || res["mimeType"] != AppResourceMimeType {
		t.Errorf("resource fields wrong: %v", res)
	}
	ui := res["_meta"].(map[string]any)["ui"].(map[string]any)
	if ui["csp"] != "default-src 'self'" {
		t.Errorf("resource csp not carried: %v", ui)
	}

	// The resource reads back the HTML.
	p, _ := json.Marshal(map[string]any{"uri": "ui://app/studio.html"})
	read := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 3, Method: "resources/read", Params: p}))
	if read["contents"].([]any)[0].(map[string]any)["text"] != "<h1>studio</h1>" {
		t.Errorf("resource HTML mismatch: %v", read)
	}
}

func TestRegisterApp_DuplicateLeavesRegistryClean(t *testing.T) {
	s := NewServer()
	if err := s.RegisterApp(sampleApp()); err != nil {
		t.Fatal(err)
	}
	// Same tool name, different resource uri → dup tool, resource NOT added.
	dup := sampleApp()
	dup.ResourceURI = "ui://app/other.html"
	if err := s.RegisterApp(dup); err == nil {
		t.Fatal("duplicate tool name should error")
	}
	rm := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/list"}))
	if n := len(rm["resources"].([]any)); n != 1 {
		t.Errorf("dup app half-registered a resource: %d resources", n)
	}
}

func TestRegisterApp_Validation(t *testing.T) {
	s := NewServer()
	base := sampleApp()

	bad := base
	bad.Name = ""
	if err := s.RegisterApp(bad); err == nil {
		t.Error("empty name should error")
	}
	bad = base
	bad.Handler = nil
	if err := s.RegisterApp(bad); err == nil {
		t.Error("nil handler should error")
	}
	bad = base
	bad.ResourceURI = ""
	if err := s.RegisterApp(bad); err == nil {
		t.Error("empty resource uri should error")
	}
	bad = base
	bad.HTML = ""
	if err := s.RegisterApp(bad); err == nil {
		t.Error("empty HTML should error")
	}
}

func TestWithResourceGate_BlocksAndAllows(t *testing.T) {
	s := NewServer()
	type ctxKey string
	const k ctxKey = "ok"
	gate := func(ctx context.Context) error {
		if ctx.Value(k) == true {
			return nil
		}
		return fmt.Errorf("forbidden")
	}
	_ = s.RegisterResource("sec://x", "X", "text/plain", func(context.Context) (ResourceContents, error) {
		return ResourceContents{Text: "secret"}, nil
	}, WithResourceGate(gate))

	p, _ := json.Marshal(map[string]any{"uri": "sec://x"})
	// Unauthorized: gate refuses, contents never read.
	deny := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
	if deny.Error == nil {
		t.Fatal("gated resource read should be refused without auth")
	}
	// Authorized: gate passes, contents returned.
	ctx := context.WithValue(context.Background(), k, true)
	allow := wireResult(t, s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 2, Method: "resources/read", Params: p}))
	if allow["contents"].([]any)[0].(map[string]any)["text"] != "secret" {
		t.Errorf("authorized read did not return contents: %v", allow)
	}
}

func TestWithResourceGate_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("WithResourceGate(nil) should panic")
		}
	}()
	_ = WithResourceGate(nil)
}

func TestRegisterApp_ToolMetaPreservesUILinkage(t *testing.T) {
	// A caller adding ui.* extras via ToolMeta must NOT clobber the
	// auto-injected ui.resourceUri linkage the App exists to create.
	s := NewServer()
	cfg := sampleApp()
	cfg.ToolMeta = map[string]any{"ui": map[string]any{"preferredSize": "large"}}
	if err := s.RegisterApp(cfg); err != nil {
		t.Fatal(err)
	}
	lm := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}))
	ui := lm["tools"].([]any)[0].(map[string]any)["_meta"].(map[string]any)["ui"].(map[string]any)
	if ui["resourceUri"] != "ui://app/studio.html" {
		t.Errorf("ToolMeta clobbered ui.resourceUri: %v", ui)
	}
	if ui["preferredSize"] != "large" {
		t.Errorf("caller ui.* extra not preserved: %v", ui)
	}
}

func TestRegisterResource_Validation(t *testing.T) {
	s := NewServer()
	f := func(context.Context) (ResourceContents, error) { return ResourceContents{}, nil }
	if err := s.RegisterResource("", "n", "m", f); err == nil {
		t.Error("empty uri should error")
	}
	if err := s.RegisterResource("u", "", "m", f); err == nil {
		t.Error("empty name should error")
	}
	if err := s.RegisterResource("u", "n", "m", nil); err == nil {
		t.Error("nil contents should error")
	}
	if err := s.RegisterResource("u", "n", "m", f); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterResource("u", "n", "m", f); err == nil {
		t.Error("duplicate uri should error")
	}
}
