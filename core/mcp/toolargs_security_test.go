package mcp

import (
	"context"
	"encoding/json"
	"runtime"
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

	// A Go []string type union is checked like the []any one: it fell
	// through to the unsupported branch and accepted any value.
	t.Run("string slice type union", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"note": map[string]any{"type": []string{"string", "null"}},
		}}
		if err, ran := call(t, schema, `{"note":{}}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] object passed a string|null union (err=%v ran=%v)", err, ran)
		}
		if _, ran := call(t, schema, `{"note":null}`); !ran {
			t.Fatal("null refused by a string|null union")
		}
	})

	// Integers above 2^53 compare by their digits: the neighbour that
	// rounds to the same float64 is refused, and the declared value is
	// accepted though it arrives as a json.Number.
	t.Run("integer enum exact", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"id": map[string]any{"type": "integer", "enum": []any{int64(9007199254740993), 7}},
		}}
		if err, ran := call(t, schema, `{"id":9007199254740992}`); err == nil || ran {
			t.Fatalf("SECURITY: [tool-args] integer outside the enum matched by float rounding (err=%v ran=%v)", err, ran)
		}
		if _, ran := call(t, schema, `{"id":9007199254740993}`); !ran {
			t.Fatal("the declared big integer was refused")
		}
		if _, ran := call(t, schema, `{"id":7}`); !ran {
			t.Fatal("small in-enum integer refused")
		}
		if _, ran := call(t, schema, `{"id":7.0}`); !ran {
			t.Fatal("7.0 is the integer 7 and is in the enum")
		}
		// In-process callers pass Go natives of any numeric type.
		for _, v := range []any{int32(7), uint8(7), uint64(7), float32(7)} {
			if err := validateToolArgs(schema, map[string]any{"id": v}); err != nil {
				t.Errorf("%T(7) refused: %v", v, err)
			}
		}
	})

	// An integral value past 2^53 keeps its caller's spelling as a
	// json.Number (normalizeArgNumbers), so integer-ness is read from the
	// digits: a point or an exponent does not make it fractional. The
	// tiny exponent must be answered from its text, never by building
	// 10^999999999.
	t.Run("integer spelled with point or exp", func(t *testing.T) {
		schema := map[string]any{"type": "object", "properties": map[string]any{
			"id": map[string]any{"type": "integer"},
		}}
		for _, ok := range []string{
			"9007199254740993", "9007199254740993e0", "9007199254740993.0",
			"9007199254740993E+0", "90071992547409930e-1", "1e999", "1e99999999999", "10e9223372036854775807", "0e-5", "-9007199254740993.000",
		} {
			if err, ran := call(t, schema, `{"id":`+ok+`}`); !ran {
				t.Errorf("integral %s refused as an integer: %v", ok, err)
			}
		}
		for _, bad := range []string{
			"9007199254740993.5", "9007199254740993e-1", "1e-999999999", "1.5", "1.0000000000000001", "1e-99999999999", "1.5e-9223372036854775808",
		} {
			if err, ran := call(t, schema, `{"id":`+bad+`}`); err == nil || ran {
				t.Errorf("SECURITY: [tool-args] fractional %s passed an integer schema (err=%v ran=%v)", bad, err, ran)
			}
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

// An enum check compares numbers by their decimal value, never by
// building them: 1e999999 survives the wire as a json.Number (it
// overflows float64), and big.Rat.SetString turned it into a ~415 KB
// integer per enum member it was compared with.
func TestToolArgsEnumHugeExponent(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"n": map[string]any{"enum": []any{1, 2.5, json.Number("7")}},
	}}
	for _, huge := range []string{"1e999999", "-1e999999", "1e-999999", "1e99999999999999999999"} {
		args := map[string]any{"n": json.Number(huge)}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		err := validateToolArgs(schema, args)
		runtime.ReadMemStats(&after)
		if err == nil {
			t.Errorf("%s matched an enum of small numbers", huge)
		}
		if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<10 {
			t.Errorf("SECURITY: [tool-args] enum check of %s allocated %d bytes; it must compare text, not build the number", huge, grew)
		}
	}
}

// Text comparison keeps the exactness the enum check needs: equal
// values in any spelling match, neighbours past 2^53 do not.
func TestToolArgsEnumExactText(t *testing.T) {
	cases := []struct {
		arg, member any
		want        bool
	}{
		{json.Number("9007199254740993"), int64(9007199254740993), true},
		{json.Number("9007199254740992"), int64(9007199254740993), false},
		{json.Number("9007199254740993e0"), int64(9007199254740993), true},
		{json.Number("9007199254740992.0"), int64(9007199254740993), false},
		{json.Number("9007199254740993"), float64(1 << 53), false},
		{json.Number("9007199254740992"), float64(1 << 53), true},
		{json.Number("1.0"), 1, true},
		{json.Number("1e2"), 100, true},
		{json.Number("10.0e1"), uint8(100), true},
		{json.Number("1.0000000000000001"), 1, false},
		{json.Number("0.5"), 0.5, true},
		{json.Number("0.1"), 0.1, false}, // 0.1 as a float64 is not one tenth
		{json.Number("-1.0"), -1, true},
		{json.Number("1"), -1, false},
		{json.Number("-0.0e5"), 0, true},
		{json.Number("1e999999"), json.Number("10e999998"), true},
		{json.Number("1e999999"), json.Number("1e1000000"), false},
		{json.Number("1e99999999999999999999"), json.Number("0.1e100000000000000000000"), true},
		{json.Number("1e99999999999999999999"), json.Number("1e99999999999999999998"), false},
		{float64(1e300), json.Number("1e300"), false}, // the float is not exactly 10^300
		{float64(1 << 60), json.Number("1152921504606846976"), true},
		{int64(-5), float32(-5), true},
		{json.Number("1E+2"), 100, true},
		{json.Number(""), 0, false},
		{json.Number("1e"), 1, false},
	}
	for _, c := range cases {
		if got := jsonEqual(c.arg, c.member); got != c.want {
			t.Errorf("jsonEqual(%v %T, %v %T) = %v, want %v", c.arg, c.arg, c.member, c.member, got, c.want)
		}
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
