package framework

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// This file adds unit coverage for the PURE tool-namespacing + schema helpers
// in processmodule_tools.go that are not already covered by the existing
// tool-surface tests.

// ---- splitModuleTool ----

func TestSplitModuleTool_validAndInvalid(t *testing.T) {
	cases := []struct {
		in   string
		mod  string
		tool string
		ok   bool
	}{
		{"module.demo.search", "demo", "search", true},
		{"module.a.b.c", "a", "b.c", true}, // tool id may contain dots after the first sep
		{"demo.search", "", "", false},     // missing module. prefix
		{"module.", "", "", false},         // no tool id after sep
		{"module.demo", "", "", false},     // no sep at all
		{"module..search", "", "", false},  // empty module name
		{"plain", "", "", false},           // not a module tool
		{"", "", "", false},
	}
	for _, c := range cases {
		mod, tool, ok := splitModuleTool(c.in)
		if mod != c.mod || tool != c.tool || ok != c.ok {
			t.Errorf("splitModuleTool(%q) = %q,%q,%t want %q,%q,%t",
				c.in, mod, tool, ok, c.mod, c.tool, c.ok)
		}
	}
}

// ---- moduleToolNamespace (round-trip with splitModuleTool) ----

func TestModuleToolNamespace_roundTrips(t *testing.T) {
	name := moduleToolNamespace("demo", "search")
	if name != "module.demo.search" {
		t.Fatalf("namespace = %q", name)
	}
	mod, tool, ok := splitModuleTool(name)
	if !ok || mod != "demo" || tool != "search" {
		t.Errorf("split round-trip = %q,%q,%t", mod, tool, ok)
	}
}

// ---- UnregisterTools (tracking-only no-op in v1) ----

func TestUnregisterTools_isNoOp(t *testing.T) {
	reg := NewModuleToolRegistry(mcp.NewServer(), nil)
	if err := reg.UnregisterTools("demo"); err != nil {
		t.Errorf("UnregisterTools returned %v; it is a v1 tracking-only no-op", err)
	}
	// owner map is intentionally preserved (gate keeps refusing).
	reg.owner["module.demo.x"] = "demo"
	_ = reg.UnregisterTools("demo")
	if _, ok := reg.owner["module.demo.x"]; !ok {
		t.Error("UnregisterTools cleared owner[] — it must stay populated in v1")
	}
}

// ---- decodeModuleToolSchema ----

func TestDecodeModuleToolSchema_emptyAndInvalid(t *testing.T) {
	if got := decodeModuleToolSchema(nil); len(got) != 0 {
		t.Errorf("decodeModuleToolSchema(nil) = %+v, want empty map", got)
	}
	if got := decodeModuleToolSchema(json.RawMessage(`not-json`)); len(got) != 0 {
		t.Errorf("decodeModuleToolSchema(garbage) = %+v, want empty map", got)
	}
	// A non-object JSON value (array) → empty map.
	if got := decodeModuleToolSchema(json.RawMessage(`[1,2]`)); len(got) != 0 {
		t.Errorf("decodeModuleToolSchema(array) = %+v, want empty map", got)
	}
}

func TestDecodeModuleToolSchema_object(t *testing.T) {
	got := decodeModuleToolSchema(json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	if got["type"] != "object" {
		t.Errorf("decodeModuleToolSchema = %+v", got)
	}
}

// ---- ModuleToolDigest edge cases ----

func TestModuleToolDigest_emptySchemaDeterministic(t *testing.T) {
	a := ModuleToolDigest(moduleproto.Tool{ID: "x", Name: "X"})
	b := ModuleToolDigest(moduleproto.Tool{ID: "x", Name: "X"})
	if a != b {
		t.Error("ModuleToolDigest not deterministic for identical tools")
	}
	// Different input → different digest.
	c := ModuleToolDigest(moduleproto.Tool{ID: "x", Name: "Y"})
	if a == c {
		t.Error("digest collision on different Name")
	}
}

func TestModuleToolDigest_schemaChangeAltersDigest(t *testing.T) {
	base := moduleproto.Tool{ID: "x", Name: "X", InputSchema: json.RawMessage(`{"type":"object"}`)}
	alt := base
	alt.InputSchema = json.RawMessage(`{"type":"string"}`)
	if ModuleToolDigest(base) == ModuleToolDigest(alt) {
		t.Error("schema change did not alter digest")
	}
}

// ---- GateForModule without supervisor (covers the nil-sup branch) ----

func TestGateForModule_nilSupervisorErrors(t *testing.T) {
	reg := NewModuleToolRegistry(mcp.NewServer(), nil)
	if err := reg.GateForModule("demo"); err == nil {
		t.Error("GateForModule with nil supervisor must error (omit tool)")
	}
}

// ---- RegisterTools collision against a non-module tool already in server ----

func TestRegisterTools_shadowedByNonModuleTool(t *testing.T) {
	srv := mcp.NewServer()
	// Pre-install an in-process tool whose name collides with the namespaced
	// form a module would claim, so RegisterTools hits the "shadowed by a
	if err := srv.RegisterTool("module.demo.x", "in-process shadow", nil, func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatalf("seed shadow tool: %v", err)
	}
	reg := NewModuleToolRegistry(srv, nil)
	err := reg.RegisterTools("demo", []moduleproto.Tool{{ID: "x", Name: "X"}})
	if err == nil {
		t.Fatal("RegisterTools must refuse a name shadowed by a non-module tool")
	}
	if !strings.Contains(err.Error(), "module.demo.x") {
		t.Errorf("collision error = %v, want namespaced name in message", err)
	}
}

// ---- errors.Is sentinel compat ----

func TestModuleToolErrors_areSentinels(t *testing.T) {
	if !errors.Is(ErrModuleToolNotReady, ErrModuleToolNotReady) {
		t.Error("ErrModuleToolNotReady must satisfy errors.Is on itself")
	}
	if errModuleToolUnavailable == nil {
		t.Error("errModuleToolUnavailable is nil")
	}
}

// ---- callerFromCtx (user-present + anonymous branches) ----

func TestCallerFromCtx_anonymousAndWithUser(t *testing.T) {
	// Anonymous: no user in context → empty Subject.
	got := callerFromCtx(context.Background(), "hdl")
	if got.Subject != "" || got.Delegation != "hdl" {
		t.Errorf("anonymous callerFromCtx = %+v", got)
	}
	// With a resolved user → Subject is its fmt.Sprint form.
	ctx := handler.SetUser(context.Background(), "user-7")
	got = callerFromCtx(ctx, "hdl2")
	if got.Subject != "user-7" || got.Delegation != "hdl2" {
		t.Errorf("callerFromCtx with user = %+v", got)
	}
}
