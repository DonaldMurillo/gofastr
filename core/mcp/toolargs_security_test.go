package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Pins tools/call forwarding non-conforming arguments to handlers
// verbatim, found by the 2026-09-04 red-probe round; fixed in
// Server.callTool validating arguments against the tool's declared
// inputSchema before invokeHandler (core/mcp/toolargs.go).
//
// Property: arguments of a tools/call must conform to the tool's
// declared inputSchema at dispatch time; a non-conforming call is
// refused with invalid-params and the handler never runs.
// Surfaces: tools.go::handleToolsCall → server.go::callTool (the
// single dispatch chokepoint; every registered tool — framework/crud's
// entity tools, processmodule's proxied module tools, battery tools —
// passes through it), plus Server.CallTool (the in-process twin of the
// same chokepoint). Per-tool opt-out: Tool.LaxArgs / WithLaxArgs skips
// validation for tools that must see their arguments verbatim.
//
// Semantics pinned here (see toolargs.go for the rationale):
//   - JSON types, required, enum, items, and additionalProperties are
//     enforced as declared.
//   - additionalProperties absent = extras allowed (JSON Schema
//     default; the convention this repo's schemas are authored under);
//     explicit false = closed; a subschema validates the extras.
//   - Bounds keywords (minimum/maximum/minLength/maxLength) are NOT
//     enforced: handlers that document clamp semantics keep them.

// dispatchViaJSONRPC exercises the transport surface: a full
// tools/call JSON-RPC request through HandleRequest.
func dispatchViaJSONRPC(t *testing.T, s *Server, args string) (Response, bool, map[string]any) {
	t.Helper()
	ran := false
	var got map[string]any
	register := func() {
		if err := s.RegisterTool("shaped", "typed tool", shapedSchema,
			func(_ context.Context, params map[string]any) (any, error) {
				ran = true
				got = params
				return "ok", nil
			}); err != nil {
			t.Fatal(err)
		}
	}
	register()
	params, _ := json.Marshal(map[string]any{"name": "shaped", "arguments": json.RawMessage(args)})
	return s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
	}), ran, got
}

var shapedSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"count": map[string]any{"type": "integer", "minimum": 0},
		"label": map[string]any{"type": "string", "maxLength": 16},
	},
	"required":             []string{"count"},
	"additionalProperties": false,
}

// TestToolCallArgsMatchInputSchema: one property (args conform to the
// declared schema or the call is refused before the handler), four
// attack shapes (type confusion, nested-object-for-scalar, missing
// required key, extra key under additionalProperties:false) plus one
// conforming control.
func TestToolCallArgsMatchInputSchema(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"string where integer declared", `{"count":"7","label":"x"}`},
		{"object where string declared", `{"count":1,"label":{"injected":true}}`},
		{"missing required count", `{"label":"x"}`},
		{"extra key under additionalProperties:false", `{"count":1,"evil":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			resp, ran, got := dispatchViaJSONRPC(t, s, tc.args)
			if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
				t.Errorf("SECURITY: [tool-args] arguments %s were not validated against the declared inputSchema (resp=%+v); the call must be refused with invalid-params before the handler runs",
					tc.args, resp)
			}
			if ran {
				t.Errorf("SECURITY: [tool-args] handler ran with non-conforming args %v", got)
			}
		})
	}

	t.Run("conforming control still runs", func(t *testing.T) {
		s := NewServer()
		resp, ran, _ := dispatchViaJSONRPC(t, s, `{"count":1,"label":"ok"}`)
		if resp.Error != nil || !ran {
			t.Fatalf("conforming arguments refused (resp=%+v ran=%v) — the schema must remain satisfiable", resp, ran)
		}
	})
}

// TestToolCallArgsSchemaKeywords pins the rest of the enforced keyword
// set and the additionalProperties semantics the repo's schemas are
// authored under.
func TestToolCallArgsSchemaKeywords(t *testing.T) {
	call := func(t *testing.T, schema map[string]any, args string, opts ...ToolOption) (*RPCError, bool) {
		t.Helper()
		ran := false
		s := NewServer()
		if err := s.RegisterTool("kw", "d", schema,
			func(_ context.Context, _ map[string]any) (any, error) { ran = true; return "ok", nil },
			opts...); err != nil {
			t.Fatal(err)
		}
		params, _ := json.Marshal(map[string]any{"name": "kw", "arguments": json.RawMessage(args)})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
		})
		if resp.Error != nil {
			if resp.Error.Code != ErrInvalidParams {
				t.Fatalf("refusal code = %d, want invalid-params (%s)", resp.Error.Code, resp.Error.Message)
			}
			return resp.Error, ran
		}
		return nil, ran
	}

	t.Run("enum enforced", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"level": map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO"}},
		}}
		if err, ran := call(t, schema, `{"level":"QUIET"}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] enum value outside the declared set reached the handler (err=%v ran=%v)", err, ran)
		}
		if _, ran := call(t, schema, `{"level":"INFO"}`); !ran {
			t.Fatal("in-enum value refused")
		}
	})

	t.Run("array items enforced", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}}
		if err, ran := call(t, schema, `{"ids":["a",7]}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] array element violating items reached the handler (err=%v ran=%v)", err, ran)
		}
		if _, ran := call(t, schema, `{"ids":["a","b"]}`); !ran {
			t.Fatal("conforming array refused")
		}
	})

	t.Run("additionalProperties absent allows extras", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"q": map[string]any{"type": "string"},
		}}
		if _, ran := call(t, schema, `{"q":"x","anything":"else"}`); !ran {
			t.Fatal("absent additionalProperties must allow extras (JSON Schema default, the convention this repo's schemas are authored under)")
		}
	})

	t.Run("additionalProperties subschema validates extras", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cmd": map[string]any{"type": "string"},
			},
			"required":             []string{"cmd"},
			"additionalProperties": map[string]any{"type": "string"},
		}
		if _, ran := call(t, schema, `{"cmd":"ls","ENV":"x"}`); !ran {
			t.Fatal("extra key conforming to the additionalProperties subschema refused")
		}
		if err, ran := call(t, schema, `{"cmd":"ls","ENV":{"nested":true}}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] extra key violating the additionalProperties subschema reached the handler (err=%v ran=%v)", err, ran)
		}
	})

	t.Run("nested object properties enforced", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"filter": map[string]any{
				"type":     "object",
				"required": []string{"field"},
				"properties": map[string]any{
					"field": map[string]any{"type": "string"},
					"value": map[string]any{"type": "string"},
				},
				"additionalProperties": false,
			},
		}}
		if err, ran := call(t, schema, `{"filter":{"value":"v"}}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] nested object missing its required key reached the handler (err=%v ran=%v)", err, ran)
		}
		if err, ran := call(t, schema, `{"filter":{"field":"f","evil":1}}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] nested extra key under additionalProperties:false reached the handler (err=%v ran=%v)", err, ran)
		}
		if _, ran := call(t, schema, `{"filter":{"field":"f","value":"v"}}`); !ran {
			t.Fatal("conforming nested object refused")
		}
	})

	t.Run("LaxArgs opts out of validation", func(t *testing.T) {
		if err, ran := call(t, shapedSchema, `{"count":"7","evil":"x"}`, WithLaxArgs()); err != nil || !ran {
			t.Fatalf("LaxArgs tool must receive its arguments verbatim (err=%v ran=%v)", err, ran)
		}
	})
}

// TestToolCallArgsInProcessChokepoint pins the same property on the
// in-process surface: Server.CallTool routes through the identical
// validation, and Go-native argument values (int, not float64) are
// typed correctly.
func TestToolCallArgsInProcessChokepoint(t *testing.T) {
	s := NewServer()
	ran := false
	if err := s.RegisterTool("ip", "d", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer"},
		},
	}, func(_ context.Context, _ map[string]any) (any, error) { ran = true; return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CallTool(context.Background(), "ip", map[string]any{"limit": "50"}); err == nil {
		t.Fatal("SECURITY: [tool-args] in-process CallTool skipped inputSchema validation: string reached a tool declaring integer")
	}
	if ran {
		t.Fatal("handler ran with non-conforming args")
	}
	// Go-native int is a JSON integer, the natural in-process spelling.
	if _, err := s.CallTool(context.Background(), "ip", map[string]any{"limit": 50}); err != nil {
		t.Fatalf("in-process call with Go int refused: %v", err)
	}
	if !ran {
		t.Fatal("conforming in-process call did not run")
	}
}

// TestToolCallArgsRefusalNamesProperty keeps the invalid-params message
// actionable: it names the offending argument so an agent can repair
// the call without trial and error.
func TestToolCallArgsRefusalNamesProperty(t *testing.T) {
	s := NewServer()
	resp, _, _ := dispatchViaJSONRPC(t, s, `{"count":"7"}`)
	if resp.Error == nil {
		t.Fatal("expected refusal")
	}
	if !strings.Contains(resp.Error.Message, "arguments.count") {
		t.Fatalf("refusal message does not name the offending argument: %q", resp.Error.Message)
	}
}
