//go:build red

package mcp

// CONTRACT-QUESTION red: tools/call currently forwards the decoded
// `arguments` object to the handler WITHOUT validating it against the
// tool's declared inputSchema — the schema tools/list advertises is purely
// descriptive. The maintainer must decide whether the dispatch chokepoint
// enforces the declared schema (refusing type-confused, missing-required and
// additionalProperty args with invalid-params before the handler runs), as
// prompts/get already does for its required arguments (prompts.go:222-230)
// and as the MCP spec's clients are told the schema means. Until decided,
// every handler — including framework/crud's entity tools — receives
// attacker-typed JSON (float64-for-int, nested-object-for-scalar) and must
// defend itself; one forgotten check is a type-confusion bug at scale.
//
// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F10 agent-tool trust boundary
// Property: arguments of a tools/call must conform to the tool's declared
// inputSchema at dispatch time; a non-conforming call is refused with
// invalid-params and the handler never runs.
// Surfaces: tools.go:handleToolsCall → server.go:callTool (the single
// dispatch chokepoint; every registered tool, including framework/crud's
// entity tools and processmodule's proxied module tools, passes through it).
// Finding: observed by running — all four non-conforming shapes below reach
// the handler verbatim and return a SUCCESS result; `ran` is true in every
// case. Only the outer params decode (name/arguments) is type-checked
// (TestParamsTypeConfusionRefused), never the arguments object itself.
// Severity: high — the declared schema is the only contract a model-driven
// caller is held to; nothing between the transport and app-supplied handlers
// enforces types, required keys, or size bounds, so each handler re-implements
// validation (battery/log's clampLimit/intParam family is the pattern every
// direct handler must copy, and a missed one is a type-confusion bug).
// Fix direction: validate req arguments against the tool's InputSchema in
// callTool (or handleToolsCall) before invokeHandler — type, required,
// additionalProperties, maxLength/maximum — refusing with ErrInvalidParams.

import (
	"context"
	"encoding/json"
	"testing"
)

// TestToolCallArgsMatchInputSchema: one property (args conform to the
// declared schema or the call is refused before the handler), four attack
// shapes (type confusion, nested-object-for-scalar, missing required key,
// extra key under additionalProperties:false) plus one conforming control.
func TestToolCallArgsMatchInputSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer", "minimum": 0},
			"label": map[string]any{"type": "string", "maxLength": 16},
		},
		"required":             []string{"count"},
		"additionalProperties": false,
	}

	newServer := func(ran *bool, got *map[string]any) *Server {
		s := NewServer()
		err := s.RegisterTool("shaped", "typed tool", schema,
			func(_ context.Context, params map[string]any) (any, error) {
				*ran = true
				*got = params
				return "ok", nil
			})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

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
			ran, got := false, map[string]any(nil)
			s := newServer(&ran, &got)
			params, _ := json.Marshal(map[string]any{"name": "shaped", "arguments": json.RawMessage(tc.args)})
			resp := s.HandleRequest(context.Background(), Request{
				JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
			})
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
		ran, got := false, map[string]any(nil)
		s := newServer(&ran, &got)
		params, _ := json.Marshal(map[string]any{"name": "shaped", "arguments": json.RawMessage(`{"count":1,"label":"ok"}`)})
		resp := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
		})
		if resp.Error != nil || !ran {
			t.Fatalf("conforming arguments refused (resp=%+v ran=%v) — the schema must remain satisfiable", resp, ran)
		}
	})
}
