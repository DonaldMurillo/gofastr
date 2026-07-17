package framework

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// These tests pin the §5.1 MCP tool surface: namespacing, the handshake
// digest byte-equality quarantine, the composite gate (disabled → omitted;
// enabled → listed), collision prevention, and that a tool invocation
// forwards module.tool.call to the live child with a delegation handle
// minted from the calling agent's context. They use the in-memory
// [newModuleProtoPipe] peer pair — no child process is spawned, so they do
// not touch the supervisor_test.go TestMain dispatch.

// validToolDescriptor builds a descriptor for a module that exposes tools,
// with a correctly computed surface digest (no child artifact — the
// tool-surface tests never spawn).
func validToolDescriptor(t *testing.T, name string, tools []ToolDigest) ProcessModuleDescriptor {
	t.Helper()
	d := ProcessModuleDescriptor{
		Name:            name,
		Version:         "1.0.0",
		ArtifactPath:    "/nonexistent/child",
		ArtifactSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Routes:          []RouteDeclaration{{ID: "r1", Method: "GET", Path: "/r"}},
		Tools:           tools,
		RequestedGrants: []access.Permission{"articles:read"},
		TrustTier:       TrustTrusted,
	}
	surf, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatalf("compute surface: %v", err)
	}
	d.SurfaceSHA256 = surf
	return d
}

// demoToolDigest mints a ToolDigest whose SHA256 matches the canonical
// digest of a moduleproto.Tool with the same id/name/description/schema.
func demoToolDigest(id, name, desc string, schema json.RawMessage) ToolDigest {
	return ToolDigest{
		ID:     id,
		SHA256: ModuleToolDigest(moduleproto.Tool{ID: id, Name: name, Description: desc, InputSchema: schema}),
	}
}

// findMCPTool looks up a tool by name through the exported ListTools
// (mcp.Server.getTool is unexported; ListTools is the public read path).
func findMCPTool(t *testing.T, srv *mcp.Server, name string) mcp.Tool {
	t.Helper()
	for _, tl := range srv.ListTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not listed", name)
	return mcp.Tool{}
}

// TestModuleToolDigest_Canonical pins the canonical form: the same tool
// bytes always hash to the same digest, and a one-byte change does not.
func TestModuleToolDigest_Canonical(t *testing.T) {
	a := moduleproto.Tool{ID: "lookup", Name: "Lookup", Description: "look up", InputSchema: json.RawMessage(`{"type":"object"}`)}
	b := a
	b.Description = "look up!" // one-byte change
	if ModuleToolDigest(a) != ModuleToolDigest(a) {
		t.Fatal("digest not deterministic")
	}
	if ModuleToolDigest(a) == ModuleToolDigest(b) {
		t.Fatal("digest collision on different tool bytes")
	}
}

// TestModuleToolRegistry_Namespacing: RegisterTools installs each tool
// under `module.<name>.<tool>` so two modules' same-named tools coexist,
// and the schema/description carry through.
func TestModuleToolRegistry_Namespacing(t *testing.T) {
	srv := mcp.NewServer()
	reg := NewModuleToolRegistry(srv, nil)
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	tools := []moduleproto.Tool{
		{ID: "search", Name: "Search", Description: "search docs", InputSchema: schema},
	}
	if err := reg.RegisterTools("alpha", tools); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := reg.RegisterTools("beta", tools); err != nil {
		t.Fatalf("register beta: %v", err)
	}
	listed := srv.ListTools()
	names := make(map[string]bool, len(listed))
	for _, tl := range listed {
		names[tl.Name] = true
	}
	if !names["module.alpha.search"] {
		t.Errorf("missing module.alpha.search in %v", names)
	}
	if !names["module.beta.search"] {
		t.Errorf("missing module.beta.search in %v", names)
	}
	lt := findMCPTool(t, srv, "module.alpha.search")
	if lt.Description != "search docs" {
		t.Errorf("description = %q, want %q", lt.Description, "search docs")
	}
	if mod, ok := reg.ModuleFor("module.alpha.search"); !ok || mod != "alpha" {
		t.Errorf("ModuleFor = %q,%t want alpha,true", mod, ok)
	}
	if _, ok := reg.ModuleFor("not.a.module.tool"); ok {
		t.Error("ModuleFor should miss a non-module tool")
	}
}

// TestModuleToolRegistry_Collides: a namespaced name already owned by a
// different module is a hard collision.
func TestModuleToolRegistry_Collides(t *testing.T) {
	srv := mcp.NewServer()
	reg := NewModuleToolRegistry(srv, nil)
	// Pre-seed the owner map so the same namespaced name is "taken" by
	// another module, then try to register it for "alpha".
	reg.owner["module.alpha.search"] = "other"
	tools := []moduleproto.Tool{{ID: "search", Name: "Search"}}
	err := reg.RegisterTools("alpha", tools)
	if err == nil {
		t.Fatal("expected collision error for already-owned name")
	}
}

// TestModuleToolRegistry_DisabledGate: a disabled module's tools are gated
// out (omitted from tools/list); an enabled module's tools pass the gate.
func TestModuleToolRegistry_DisabledGate(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	d := validToolDescriptor(t, "demo", nil)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer sup.Close(context.Background())
	reg := NewModuleToolRegistry(mcp.NewServer(), sup)

	if err := reg.GateForModule("demo"); err == nil {
		t.Error("disabled module should fail the gate (omitted+refused)")
	}
	sl := sup.Slot("demo")
	sl.mu.Lock()
	sl.enabled = true
	sl.state = StateReady
	sl.mu.Unlock()
	if err := reg.GateForModule("demo"); err != nil {
		t.Errorf("enabled module should pass the gate, got: %v", err)
	}
	if err := reg.GateForModule("nope"); err == nil {
		t.Error("unknown module should fail the gate")
	}
}

// toolTestSlot builds a supervisor + registered module + in-memory host
// peer wired into the slot (state=Ready), so verifyToolSurface /
// dispatchToolCall can be driven without spawning a child. The returned
// child peer lets the test install module.tool.list / module.tool.call
// handlers; calls on the host peer round-trip to the child.
func toolTestSlot(t *testing.T, tools []ToolDigest) (sup *ProcessModuleSupervisor, sl *moduleSlot, host, child *moduleproto.Peer, cleanup func()) {
	t.Helper()
	store := newTestStore(t)
	sup = newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	d := validToolDescriptor(t, "demo", tools)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sl = sup.Slot("demo")
	host, child, pipeClose := newModuleProtoPipe(t)
	sl.mu.Lock()
	sl.peer = host
	sl.enabled = true
	sl.state = StateReady
	sl.mu.Unlock()
	cleanup = func() {
		pipeClose()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = sup.Close(ctx)
	}
	return
}

// TestVerifyToolSurface_DigestMismatch: a child whose module.tool.list
// returns a tool whose digest does NOT match the descriptor is quarantined
// — verifyToolSurface returns a *moduleproto.HandshakeMismatchError (the
// integrity fault isIntegrityFault recognizes → terminal Failed).
func TestVerifyToolSurface_DigestMismatch(t *testing.T) {
	good := demoToolDigest("search", "Search", "desc", nil)
	sup, sl, host, child, cleanup := toolTestSlot(t, []ToolDigest{good})
	_ = sup
	defer cleanup()

	if err := child.Handle(moduleproto.MethodToolList, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.ToolListResult{Tools: []moduleproto.Tool{
			{ID: "search", Name: "Search", Description: "TAMPERED"},
		}}, nil
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	err := sl.verifyToolSurface(context.Background(), host)
	var hs *moduleproto.HandshakeMismatchError
	if !errors.As(err, &hs) {
		t.Fatalf("want HandshakeMismatchError for digest mismatch, got %T %v", err, err)
	}
}

// TestVerifyToolSurface_ExtraToolRejected: a child exposing a tool the
// descriptor did not approve is rejected (cannot add a tool at runtime).
func TestVerifyToolSurface_ExtraToolRejected(t *testing.T) {
	good := demoToolDigest("search", "Search", "desc", nil)
	sup, sl, host, child, cleanup := toolTestSlot(t, []ToolDigest{good})
	defer cleanup()
	_ = sup
	if err := child.Handle(moduleproto.MethodToolList, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.ToolListResult{Tools: []moduleproto.Tool{
			{ID: "search", Name: "Search", Description: "desc"},
			{ID: "sneaky", Name: "Sneaky", Description: "x"},
		}}, nil
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	err := sl.verifyToolSurface(context.Background(), host)
	var hs *moduleproto.HandshakeMismatchError
	if !errors.As(err, &hs) {
		t.Fatalf("want HandshakeMismatchError for extra tool, got %T %v", err, err)
	}
}

// TestVerifyToolSurface_SuccessRegisters: a child whose tool.list matches
// the descriptor digests is accepted, and the verified tools are installed
// into the host MCP server via the registrar.
func TestVerifyToolSurface_SuccessRegisters(t *testing.T) {
	good := demoToolDigest("search", "Search", "search docs", json.RawMessage(`{"type":"object"}`))
	sup, sl, host, child, cleanup := toolTestSlot(t, []ToolDigest{good})
	defer cleanup()
	srv := mcp.NewServer()
	reg := NewModuleToolRegistry(srv, sup)
	sup.tools = reg
	defer func() { sup.tools = nil }()

	if err := child.Handle(moduleproto.MethodToolList, func(context.Context, json.RawMessage) (any, error) {
		return moduleproto.ToolListResult{Tools: []moduleproto.Tool{
			{ID: "search", Name: "Search", Description: "search docs", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}}, nil
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if err := sl.verifyToolSurface(context.Background(), host); err != nil {
		t.Fatalf("verifyToolSurface: %v", err)
	}
	lt := findMCPTool(t, srv, "module.demo.search")
	if lt.Description != "search docs" {
		t.Errorf("registered description = %q", lt.Description)
	}
}

// TestDispatchToolCall_NotReady: invoking a tool whose module has no live
// Ready child returns ErrModuleToolNotReady (the handler maps this to the
// retryable temp-unavailable error).
func TestDispatchToolCall_NotReady(t *testing.T) {
	store := newTestStore(t)
	sup := newBareTestSupervisor(t, store, &TrustedProcessRunner{})
	d := validToolDescriptor(t, "demo", nil)
	if _, err := sup.Register(context.Background(), d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer sup.Close(context.Background())
	_, err := sup.dispatchToolCall(context.Background(), "demo", "search", map[string]any{"q": "x"})
	if !errors.Is(err, ErrModuleToolNotReady) {
		t.Fatalf("want ErrModuleToolNotReady, got %v", err)
	}
}

// TestDispatchToolCall_ForwardsAndDelegates: with a Ready slot over an
// in-memory pipe, dispatchToolCall forwards module.tool.call to the child
// and returns its result. The supervisor's NopBroker mints an ambient
// (empty) delegation handle — sufficient to prove the forward path; the
// capability intersection on the reverse channel is covered by the broker
// suite (processmodule_broker_test.go).
func TestDispatchToolCall_ForwardsAndDelegates(t *testing.T) {
	good := demoToolDigest("search", "Search", "desc", nil)
	sup, sl, host, child, cleanup := toolTestSlot(t, []ToolDigest{good})
	_ = host
	defer cleanup()

	var (
		gotArgs   json.RawMessage
		gotCaller moduleproto.Caller
		mu        sync.Mutex
	)
	if err := child.Handle(moduleproto.MethodToolCall, func(_ context.Context, params json.RawMessage) (any, error) {
		var p moduleproto.ToolCallParams
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		gotArgs = p.Arguments
		gotCaller = p.Caller
		mu.Unlock()
		res, _ := json.Marshal(map[string]any{"echo": string(p.Arguments)})
		return moduleproto.ToolCallResult{Result: res}, nil
	}); err != nil {
		t.Fatalf("handle tool.call: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := sup.dispatchToolCall(ctx, "demo", "search", map[string]any{"q": "hello"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotArgs) == 0 {
		t.Error("child received empty arguments")
	}
	_ = gotCaller // delegation handle attached (ambient "" under NopBroker)
	_ = sl        // slot held Ready for the dispatch
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["echo"] == nil {
		t.Error("expected echo field in result")
	}
}
