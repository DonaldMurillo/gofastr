package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/scrubtest"
)

func noopTool(context.Context, map[string]any) (any, error) { return "ok", nil }

func listedTools(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list: %v", resp.Error)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]any{}
	for _, tool := range out.Tools {
		byName[tool["name"].(string)] = tool
	}
	return byName
}

func TestToolAnnotationsServedInToolsList(t *testing.T) {
	s := NewServer()
	readOnly := Bool(true)
	if err := s.RegisterTool("lookup", "Look up", map[string]any{"type": "object"}, noopTool,
		WithAnnotations(Annotations{
			Title:           "Look something up",
			ReadOnlyHint:    readOnly,
			DestructiveHint: Bool(false),
			OpenWorldHint:   Bool(false),
		})); err != nil {
		t.Fatal(err)
	}
	*readOnly = false
	if err := s.RegisterTool("plain", "Plain", map[string]any{"type": "object"}, noopTool); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp(AppConfig{
		Name: "widget", Description: "Widget", InputSchema: map[string]any{"type": "object"}, Handler: noopTool,
		ResourceURI: "ui://w/app.html", HTML: "<p>hi</p>",
		Annotations: &Annotations{ReadOnlyHint: Bool(true)},
	}); err != nil {
		t.Fatal(err)
	}

	tools := listedTools(t, s)
	got, ok := tools["lookup"]["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("lookup has no annotations object: %#v", tools["lookup"])
	}
	want := map[string]any{"title": "Look something up", "readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
	if len(got) != len(want) {
		t.Errorf("annotations = %#v, want exactly %#v (an unset hint is omitted)", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("annotations[%q] = %#v, want %#v", k, got[k], v)
		}
	}
	if _, present := tools["plain"]["annotations"]; present {
		t.Errorf("a tool registered without annotations serves %#v", tools["plain"]["annotations"])
	}
	if app, _ := tools["widget"]["annotations"].(map[string]any); app["readOnlyHint"] != true {
		t.Errorf("RegisterApp dropped its annotations: %#v", tools["widget"])
	}
}

func TestAnnotationWarningsNameMissingHints(t *testing.T) {
	sink := &scrubtest.Sink{}
	scrubtest.InstallDefault(t, sink)
	full := WithAnnotations(Annotations{ReadOnlyHint: Bool(true), DestructiveHint: Bool(false), OpenWorldHint: Bool(false)})

	s := NewServer()
	if err := s.RegisterTool("quiet_off", "", nil, noopTool); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "" {
		t.Fatalf("warned with warnings off: %#v", sink.Attrs())
	}

	s.SetAnnotationWarnings(true)
	if err := s.RegisterTool("bare", "", nil, noopTool); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "bare" || sink.Get("missing") != "readOnlyHint, destructiveHint, openWorldHint" {
		t.Fatalf("bare tool warning = %#v", sink.Attrs())
	}
	sink.Reset()
	if err := s.RegisterTool("partial", "", nil, noopTool,
		WithAnnotations(Annotations{ReadOnlyHint: Bool(false), IdempotentHint: Bool(true)})); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "partial" || sink.Get("missing") != "destructiveHint, openWorldHint" {
		t.Fatalf("partial tool warning = %#v", sink.Attrs())
	}
	sink.Reset()
	if err := s.RegisterTool("full", "", nil, noopTool, full); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterTool("dev_only", "", nil, noopTool, WithDevImplied()); err != nil {
		t.Fatal(err)
	}
	if sink.Get("tool") != "" {
		t.Fatalf("warned for a fully hinted or dev-implied tool: %#v", sink.Attrs())
	}
}

func TestInitializeServesInstructions(t *testing.T) {
	s := NewServer()
	initialize := func() map[string]any {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
		if resp.Error != nil {
			t.Fatalf("initialize: %v", resp.Error)
		}
		return resp.Result.(map[string]any)
	}
	if _, present := initialize()["instructions"]; present {
		t.Fatal("initialize serves instructions nobody set")
	}
	s.SetInstructions("Generate barcodes.")
	if got := initialize()["instructions"]; got != "Generate barcodes." || s.Instructions() != "Generate barcodes." {
		t.Fatalf("instructions = %#v", got)
	}
	s.SetInstructions("")
	if _, present := initialize()["instructions"]; present {
		t.Fatal("clearing the instructions still serves them")
	}
}

func callWithParams(s *Server, ctx context.Context, params string) Response {
	return s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: json.RawMessage(params)})
}

func TestRequestMetaReachesHandlerAsCopy(t *testing.T) {
	s := NewServer()
	var seen, again map[string]any
	if err := s.RegisterTool("echo", "", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		seen = RequestMeta(ctx)
		if seen != nil {
			seen["openai/subject"] = "forged"
			seen["openai/userLocation"].(map[string]any)["city"] = "forged"
		}
		again = RequestMeta(ctx)
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}

	resp := callWithParams(s, context.Background(), `{"name":"echo","arguments":{},"_meta":{"openai/subject":"anon-1","openai/locale":"fr-FR","openai/userLocation":{"city":"Lyon"},"n":3}}`)
	if resp.Error != nil {
		t.Fatalf("tools/call: %v", resp.Error)
	}
	if again["openai/subject"] != "anon-1" || again["openai/locale"] != "fr-FR" {
		t.Fatalf("second RequestMeta = %#v, want the sent values", again)
	}
	if city := again["openai/userLocation"].(map[string]any)["city"]; city != "Lyon" {
		t.Fatalf("a handler's write to a nested value reached the next reader: %v", city)
	}
	if again["n"] != float64(3) {
		t.Errorf("numeric _meta value = %#v, want float64 like arguments", again["n"])
	}

	outer := ContextWithRequestMeta(context.Background(), map[string]any{"openai/subject": "outer"})
	for _, params := range []string{`{"name":"echo"}`, `{"name":"echo","_meta":null}`} {
		seen, again = map[string]any{}, map[string]any{}
		if resp := callWithParams(s, outer, params); resp.Error != nil {
			t.Fatalf("tools/call %s: %v", params, resp.Error)
		}
		if seen != nil || again != nil {
			t.Fatalf("%s: RequestMeta = %#v, want nil when the call sent none", params, again)
		}
	}
}

func TestRequestMetaRefusals(t *testing.T) {
	s := NewServer()
	ran := false
	if err := s.RegisterTool("echo", "", nil, func(context.Context, map[string]any) (any, error) {
		ran = true
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	huge := `{"name":"echo","_meta":{"k":"` + strings.Repeat("a", maxRequestMetaBytes) + `"}}`
	for name, params := range map[string]string{
		"string": `{"name":"echo","_meta":"x"}`,
		"array":  `{"name":"echo","_meta":[1]}`,
		"number": `{"name":"echo","_meta":1}`,
		"huge":   huge,
	} {
		t.Run(name, func(t *testing.T) {
			ran = false
			resp := callWithParams(s, context.Background(), params)
			if resp.Error == nil || resp.Error.Code != ErrInvalidParams || !strings.Contains(resp.Error.Message, "_meta") {
				t.Fatalf("response = %+v, want -32602 naming _meta", resp.Error)
			}
			if ran {
				t.Fatal("the handler ran on a refused _meta")
			}
		})
	}
}

func TestContextWithRequestMetaCopies(t *testing.T) {
	meta := map[string]any{"openai/locale": "en-US", "list": []any{"a"}}
	ctx := ContextWithRequestMeta(context.Background(), meta)
	meta["openai/locale"] = "changed"
	meta["list"].([]any)[0] = "changed"
	got := RequestMeta(ctx)
	if got["openai/locale"] != "en-US" || got["list"].([]any)[0] != "a" {
		t.Fatalf("RequestMeta = %#v, want the values as passed", got)
	}
	if RequestMeta(context.Background()) != nil {
		t.Fatal("RequestMeta on a bare context is not nil")
	}
}
