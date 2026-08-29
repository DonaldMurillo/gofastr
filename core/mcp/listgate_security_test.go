package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type ctxKey string

const principalKey ctxKey = "principal"

func authed(ctx context.Context) context.Context {
	return context.WithValue(ctx, principalKey, "u1")
}

func requireUser(ctx context.Context) error {
	if ctx.Value(principalKey) == nil {
		return errors.New("authentication required")
	}
	return nil
}

// tools/list ran with no gate at all: mcp.Gated wraps HANDLERS, so it only
// ever affected tools/call. An unauthenticated POST therefore returned every
// tool's inputSchema, which framework/crud builds from live entity
// definitions, i.e. every entity name, every non-Hidden field, its type and
// full enum set. The call 401s; the schema was already out.
func TestGatedToolIsHiddenFromUnauthenticatedList(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)
	mustRegisterOpen(t, s, "public_tool")

	names := listToolNames(t, s, context.Background())
	if contains(names, "secret_tool") {
		t.Errorf("SECURITY: [disclosure] unauthenticated tools/list exposed a gated tool's schema; got %v", names)
	}
	if !contains(names, "public_tool") {
		t.Errorf("ungated tool vanished from the listing; got %v", names)
	}
}

// The caller who can actually call it must still see it, or the gate has
// merely broken discovery.
func TestGatedToolIsVisibleToAuthenticatedList(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)

	names := listToolNames(t, s, authed(context.Background()))
	if !contains(names, "secret_tool") {
		t.Errorf("authenticated tools/list hid a callable tool; got %v", names)
	}
}

// Hiding it from the listing is disclosure control, not access control. The
// gate must also refuse the call.
func TestGatedToolRefusesUnauthenticatedCall(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)

	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"secret_tool","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] gated tool ran for an unauthenticated caller")
	}
}

// A server-wide gate closes the whole JSON-RPC data surface for hosts whose
// /mcp is private, without per-tool wiring.
func TestServerGateClosesListAndCall(t *testing.T) {
	s := NewServer()
	mustRegisterOpen(t, s, "public_tool")
	s.SetGate(requireUser)

	if names := listToolNames(t, s, context.Background()); len(names) != 0 {
		t.Errorf("SECURITY: [disclosure] server gate did not cover tools/list; got %v", names)
	}
	resp := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"public_tool","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Error("SECURITY: [authz] server gate did not cover tools/call")
	}

	// resources/list is the same disclosure surface by another name.
	rl := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 2, Method: "resources/list"})
	if rl.Error == nil {
		t.Error("SECURITY: [disclosure] server gate did not cover resources/list")
	}

	if names := listToolNames(t, s, authed(context.Background())); !contains(names, "public_tool") {
		t.Errorf("server gate refused an authenticated caller; got %v", names)
	}
}

// The handshake stays open on purpose: it carries only the protocol version,
// capability booleans and the server name, and a client that cannot handshake
// cannot present credentials in a way any MCP client implements. Pin the
// decision so a future change is deliberate.
func TestInitializeAndPingStayOpenUnderServerGate(t *testing.T) {
	s := NewServer()
	s.SetGate(requireUser)
	for _, method := range []string{"initialize", "ping"} {
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method})
		if resp.Error != nil {
			t.Errorf("%s refused under the server gate: %v", method, resp.Error)
		}
	}
	// …and it must not carry anything but the handshake.
	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	blob, _ := json.Marshal(resp.Result)
	if strings.Contains(string(blob), "inputSchema") {
		t.Error("SECURITY: [disclosure] initialize leaked tool schemas")
	}
}

// Same property, prompts surface: a gated prompt does not leak through
// prompts/list, and a gated tool's companion prompt (the pairing MCP Apps
// and slash-command surfaces produce: tool + prompt sharing a gate) does
// not leak either. The description and argument list are the disclosure,
// the same way a tool's inputSchema is.
func TestGatedPromptHiddenFromList(t *testing.T) {
	s := NewServer()
	mustRegisterGated(t, s, "secret_tool", requireUser)
	mustRegisterPrompt(t, s, "secret_prompt",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil },
		WithPromptDescription("Companion to secret_tool"),
		WithPromptGate(requireUser))
	mustRegisterPrompt(t, s, "public_prompt",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil })

	names := listPromptNames(t, s, context.Background())
	if contains(names, "secret_prompt") {
		t.Errorf("SECURITY: [disclosure] unauthenticated prompts/list exposed a gated prompt; got %v", names)
	}
	if !contains(names, "public_prompt") {
		t.Errorf("ungated prompt vanished from the listing; got %v", names)
	}

	// The caller who passes the gate must still see it.
	names = listPromptNames(t, s, authed(context.Background()))
	if !contains(names, "secret_prompt") {
		t.Errorf("authenticated prompts/list hid a gettable prompt; got %v", names)
	}
}

// The server-wide gate covers the prompt data surface too: prompts/list and
// prompts/get sit in the gate switch alongside the tool and resource
// methods, so a private /mcp cannot be read through the newest surface.
func TestServerGateClosesPromptsSurface(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "public_prompt",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil })
	s.SetGate(requireUser)

	rl := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
	if rl.Error == nil {
		t.Error("SECURITY: [disclosure] server gate did not cover prompts/list")
	}
	rg := s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 2, Method: "prompts/get",
		Params: json.RawMessage(`{"name":"public_prompt"}`),
	})
	if rg.Error == nil {
		t.Error("SECURITY: [authz] server gate did not cover prompts/get")
	}

	if names := listPromptNames(t, s, authed(context.Background())); !contains(names, "public_prompt") {
		t.Errorf("server gate refused an authenticated caller; got %v", names)
	}
}

// The server-wide gate covers the resource-template surface too:
// resources/templates/list sits in the gate switch alongside the tool,
// resource and prompt methods, so a private /mcp cannot be enumerated
// through the newest data surface.
func TestServerGateClosesTemplatesSurface(t *testing.T) {
	s := NewServer()
	mustRegisterTemplate(t, s, "ui://pub/{id}", "pub")
	s.SetGate(requireUser)

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/templates/list"})
	if resp.Error == nil {
		t.Error("SECURITY: [disclosure] server gate did not cover resources/templates/list")
	}
	if uris := listTemplateURIs(t, s, authed(context.Background())); !contains(uris, "ui://pub/{id}") {
		t.Errorf("server gate refused an authenticated caller; got %v", uris)
	}
}

// A gated template (WithResourceTemplateGate) does not leak through
// resources/templates/list. The uriTemplate and description are the
// disclosure — a template names internal URI shapes the way a tool's
// inputSchema names internal entities.
func TestGatedTemplateHiddenFromList(t *testing.T) {
	s := NewServer()
	mustRegisterTemplate(t, s, "ui://secret/{id}", "secret", WithResourceTemplateGate(requireUser))
	mustRegisterTemplate(t, s, "ui://pub/{id}", "pub")

	uris := listTemplateURIs(t, s, context.Background())
	if contains(uris, "ui://secret/{id}") {
		t.Errorf("SECURITY: [disclosure] unauthenticated resources/templates/list exposed a gated template; got %v", uris)
	}
	if !contains(uris, "ui://pub/{id}") {
		t.Errorf("ungated template vanished from the listing; got %v", uris)
	}

	// The caller who passes the gate must still see it.
	uris = listTemplateURIs(t, s, authed(context.Background()))
	if !contains(uris, "ui://secret/{id}") {
		t.Errorf("authenticated resources/templates/list hid a visible template; got %v", uris)
	}
}

// Pagination must page the POST-GATE set, never the raw registry. Paging
// the unfiltered set and dropping gated items from each page leaks their
// existence twice: a short middle page tells the client an item was
// withheld, and the page count times the page size discloses how many.
// Here public and gated names interleave and every public page is
// exactly full, so any pre-filter paging shows up as a short middle page
// (or a gated name on a page).
func TestPagingNeverRevealsGatedItems(t *testing.T) {
	s := NewServer()
	s.listPageSize = 2
	var publicTools, publicPrompts, publicTemplates []string
	var hiddenTools, hiddenPrompts, hiddenTemplates []string
	for i := range 7 { // p0 p2 p4 p6 public; p1 p3 p5 gated
		name := fmt.Sprintf("p%d", i)
		if i%2 == 1 {
			mustRegisterGated(t, s, name, requireUser)
			mustRegisterPrompt(t, s, name, noopPrompt, WithPromptGate(requireUser))
			mustRegisterTemplate(t, s, "ui://"+name+"/{id}", name, WithResourceTemplateGate(requireUser))
			hiddenTools = append(hiddenTools, name)
			hiddenPrompts = append(hiddenPrompts, name)
			hiddenTemplates = append(hiddenTemplates, "ui://"+name+"/{id}")
		} else {
			mustRegisterOpen(t, s, name)
			mustRegisterPrompt(t, s, name, noopPrompt)
			mustRegisterTemplate(t, s, "ui://"+name+"/{id}", name)
			publicTools = append(publicTools, name)
			publicPrompts = append(publicPrompts, name)
			publicTemplates = append(publicTemplates, "ui://"+name+"/{id}")
		}
	}
	assertPagedVisibility(t, s, "tools/list", "tools", "name", publicTools, hiddenTools)
	assertPagedVisibility(t, s, "prompts/list", "prompts", "name", publicPrompts, hiddenPrompts)
	assertPagedVisibility(t, s, "resources/templates/list", "resourceTemplates", "uriTemplate", publicTemplates, hiddenTemplates)
}

// assertPagedVisibility walks a paginated list as the unauthenticated
// caller and checks the post-gate paging contract: no hidden item on any
// page, every non-final page exactly the page size, and the walked set
// equal to the public set (all of it, nothing else).
func assertPagedVisibility(t *testing.T, s *Server, method, key, field string, public, hidden []string) {
	t.Helper()
	pages := walkList(t, s, context.Background(), method)
	var got []string
	for i, p := range pages {
		names := pageFieldNames(p, key, field)
		got = append(got, names...)
		for _, h := range hidden {
			if contains(names, h) {
				t.Errorf("SECURITY: [disclosure] %s page %d/%d exposed gated item %q", method, i+1, len(pages), h)
			}
		}
		if i < len(pages)-1 && len(names) != s.pageListSize() {
			t.Errorf("%s page %d/%d held %d items (page size %d): a short middle page discloses a withheld item",
				method, i+1, len(pages), len(names), s.pageListSize())
		}
	}
	if wantPages := (len(public) + s.pageListSize() - 1) / s.pageListSize(); len(pages) != wantPages {
		t.Errorf("%s: got %d pages, want %d", method, len(pages), wantPages)
	}
	slices.Sort(got)
	if !slices.Equal(got, public) {
		t.Errorf("%s: walked set = %v, want the public set %v", method, got, public)
	}
}

func mustRegisterGated(t *testing.T, s *Server, name string, gate func(context.Context) error) {
	t.Helper()
	err := s.RegisterTool(name, "d", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return "ok", nil },
		WithToolGate(gate))
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func mustRegisterOpen(t *testing.T, s *Server, name string) {
	t.Helper()
	err := s.RegisterTool(name, "d", map[string]any{"type": "object"},
		func(context.Context, map[string]any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
}

func listToolNames(t *testing.T, s *Server, ctx context.Context) []string {
	t.Helper()
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if resp.Error != nil {
		return nil
	}
	res, ok := resp.Result.(toolsListResult)
	if !ok {
		t.Fatalf("tools/list result type %T", resp.Result)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func contains(hay []string, needle string) bool {
	return slices.Contains(hay, needle)
}
