package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

// Property: a per-caller gate is app-supplied callback code and must run
// OUTSIDE the registry lock. listTools, handlePromptsList and
// handleResourcesTemplatesList all evaluate the gate while holding
// s.mu.RLock, so one slow gate (an auth check doing I/O under
// attacker-induced load) blocks every registration — and because Go's
// RWMutex is writer-preferring, a queued writer blocks new readers too:
// the whole data surface stalls behind one caller's gate.
// notifications.go states the package's own rule in the negative: fan-out
// "must never contend with (or nest inside) the registry lock".
func TestListingGateNeverBlocksRegistry(t *testing.T) {
	surfaces := []struct {
		name   string
		method string
		gated  func(s *Server, gate func(context.Context) error)
		writer func(s *Server) error
	}{
		{
			name:   "tools/list",
			method: "tools/list",
			gated:  func(s *Server, g func(context.Context) error) { mustRegisterGated(t, s, "gated", g) },
			writer: func(s *Server) error {
				return s.RegisterTool("writer", "d", nil, func(context.Context, map[string]any) (any, error) { return nil, nil })
			},
		},
		{
			name:   "prompts/list",
			method: "prompts/list",
			gated: func(s *Server, g func(context.Context) error) {
				mustRegisterPrompt(t, s, "gated", noopPrompt, WithPromptGate(g))
			},
			writer: func(s *Server) error { return s.RegisterPrompt("writer", noopPrompt) },
		},
		{
			name:   "resources/templates/list",
			method: "resources/templates/list",
			gated: func(s *Server, g func(context.Context) error) {
				mustRegisterTemplate(t, s, "ui://gated/{id}", "gated", WithResourceTemplateGate(g))
			},
			writer: func(s *Server) error {
				return s.RegisterResourceTemplate("ui://writer/{id}", "writer", "text/markdown")
			},
		},
	}

	for _, sf := range surfaces {
		t.Run(sf.name, func(t *testing.T) {
			s := NewServer()
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			blocking := func(context.Context) error {
				entered <- struct{}{}
				<-release
				return nil
			}
			sf.gated(s, blocking)

			listDone := make(chan Response, 1)
			go func() {
				listDone <- s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: sf.method})
			}()
			<-entered // the listing is now inside its gate, holding the registry

			regDone := make(chan error, 1)
			go func() { regDone <- sf.writer(s) }()
			select {
			case err := <-regDone:
				if err != nil {
					t.Fatalf("unrelated registration failed: %v", err)
				}
				close(release)
				<-listDone
			case <-time.After(750 * time.Millisecond):
				t.Errorf("SECURITY: [DoS] registration stalled behind a blocked %s gate: per-caller gates must not run under the registry lock", sf.method)
				// Drain the writer that arrived late so no goroutine
				// outlives the subtest mid-registration. (regDone carries
				// exactly one send; on the success branch above the select
				// has already consumed it, so a second receive there would
				// deadlock a correctly behaving build.)
				close(release)
				<-listDone
				if err := <-regDone; err != nil {
					t.Fatalf("late registration failed: %v", err)
				}
			}
		})
	}

	// The panic flavour of the same root: listTools releases the RLock
	// with a plain call (no defer), so a gate that panics unwinds past
	// the release and the registry stays read-locked forever.
	t.Run("panicking gate wedges registry", func(t *testing.T) {
		s := NewServer()
		mustRegisterGated(t, s, "gated", func(context.Context) error { panic("gate boom") })
		func() {
			defer func() { _ = recover() }() // observe how far the panic gets
			_ = s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
		}()
		regDone := make(chan error, 1)
		go func() {
			regDone <- s.RegisterTool("after", "d", nil, func(context.Context, map[string]any) (any, error) { return nil, nil })
		}()
		select {
		case err := <-regDone:
			if err != nil {
				t.Fatalf("registration after a recovered gate panic failed: %v", err)
			}
		case <-time.After(750 * time.Millisecond):
			t.Errorf("SECURITY: [DoS] registry wedged after a gate panic: the RLock taken around the gate was never released")
		}
	})
}

// Property: the REGISTRY-WIDE call gate is app-supplied callback code
// too, and must run OUTSIDE the registry lock. listTools and
// listToolsUnfiltered evaluated runCallGate while still holding
// s.mu.RLock, so a gate that (directly, or via a module toggle that
// ends in RegisterTool) re-entered the registry deadlocked it: the
// gate cannot return while the write lock waits, and the read lock
// cannot release until the gate returns. Same contract the per-caller
// gate test above pins, on the surfaces it did not cover.
func TestCallGateNeverBlocksRegistry(t *testing.T) {
	surfaces := []struct {
		name string
		list func(s *Server)
	}{
		{
			name: "listTools",
			list: func(s *Server) { _, _ = s.ListToolsFor(context.Background()) },
		},
		{
			name: "listToolsUnfiltered",
			list: func(s *Server) { _ = s.ListTools() },
		},
	}
	for _, sf := range surfaces {
		t.Run(sf.name, func(t *testing.T) {
			s := NewServer()
			mustRegisterOpen(t, s, "open")
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			s.SetCallGate(func(string) error {
				entered <- struct{}{}
				<-release
				return nil
			})

			listDone := make(chan struct{}, 1)
			go func() {
				sf.list(s)
				listDone <- struct{}{}
			}()
			<-entered // the listing is now inside its call gate

			regDone := make(chan error, 1)
			go func() {
				regDone <- s.RegisterTool("writer", "d", nil, func(context.Context, map[string]any) (any, error) { return nil, nil })
			}()
			select {
			case err := <-regDone:
				if err != nil {
					t.Fatalf("unrelated registration failed: %v", err)
				}
				close(release)
				<-listDone
			case <-time.After(750 * time.Millisecond):
				t.Errorf("SECURITY: [DoS] registration stalled behind a blocked %s call gate: the registry-wide gate must not run under the registry lock", sf.name)
				close(release)
				<-listDone
				if err := <-regDone; err != nil {
					t.Fatalf("late registration failed: %v", err)
				}
			}
		})
	}
}

// Property: a panicking gate fails closed as a well-formed refusal at
// every gate evaluation surface — the package's own stated contract
// (checkPromptGate: "a panicking gate must become a well-formed error,
// never a transport crash"; gateAllows: same for delivery time).
// prompts/get is already pinned by TestPromptsGetPanicBecomesError's
// boom_gate case; these are the surfaces it does not reach.
// (Deliberate shape, pinned here: a panicking gate on a LIST surface —
// tools/list / prompts/list filtering via gateRefused — fails closed
// as a filtered listing, not an internal-error response, and the
// server-wide gate's recovered panic answers as checkServerGate's
// generic refusal; the 2026-09 round-3 probes that demanded an
// ErrInternalError response there were deleted as over-specified.)
func TestPanickingGateFailsClosedEverywhere(t *testing.T) {
	t.Run("tools/call", func(t *testing.T) {
		s := NewServer()
		mustRegisterGated(t, s, "t", func(context.Context) error { panic("gate boom") })
		panicked := func() (p bool) {
			defer func() {
				if recover() != nil {
					p = true
				}
			}()
			params, _ := json.Marshal(map[string]any{"name": "t"})
			_ = s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
			return false
		}()
		if panicked {
			t.Errorf("SECURITY: [robustness] a panicking tool gate escaped tools/call: callTool runs t.Gate outside any recover guard (invokeHandler recovers only the handler) — on stdio this crashes the process")
		}
	})
	t.Run("resources/read", func(t *testing.T) {
		s := NewServer()
		ran := false
		if err := s.RegisterResource("gate://x", "X", "text/plain", func(context.Context) (ResourceContents, error) {
			ran = true
			return ResourceContents{Text: "secret"}, nil
		}, WithResourceGate(func(context.Context) error { panic("gate boom") })); err != nil {
			t.Fatal(err)
		}
		p, _ := json.Marshal(map[string]any{"uri": "gate://x"})
		resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "resources/read", Params: p})
		if resp.Error == nil || resp.Error.Code != ErrInternalError {
			t.Fatalf("panicking resource gate must become an internal-error response, got %+v", resp)
		}
		if ran {
			t.Error("contents func ran behind a panicking gate")
		}
	})
	t.Run("notification delivery", func(t *testing.T) {
		s := NewServer()
		s.SetGate(func(context.Context) error { panic("gate boom") })
		sub := s.addSSESubscriber(context.Background())
		defer s.removeSSESubscriber(sub)
		panicked, ok := func() (panicked, ok bool) {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			return false, s.subscriberMayReceive(sub, sseNotification{method: "notifications/tools/list_changed"})
		}()
		if panicked {
			t.Error("SECURITY: a panicking server gate must be recovered at delivery time (gateAllows), not unwind the stream loop")
		}
		if ok {
			t.Error("a panicking gate must fail closed: the notification must be refused")
		}
	})
}

// Property: a panicking WithToolGate on tools/call is recovered by
// checkToolGate into a well-formed internal-error response (code
// ErrInternalError, generic message), the handler never runs, and the
// panic value is not echoed — the listing-side twin of the subtest
// above, which pins only that no panic escapes.
func TestToolGatePanicBecomesInternalError(t *testing.T) {
	const panicSecret = "super-secret-gate-detail"
	s := NewServer()
	ran := false
	if err := s.RegisterTool("t", "d", nil,
		func(_ context.Context, _ map[string]any) (any, error) { ran = true; return "ok", nil },
		WithToolGate(func(context.Context) error { panic(panicSecret) }),
	); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	var rec any
	resp := func() Response {
		defer func() { rec = recover() }()
		params, _ := json.Marshal(map[string]any{"name": "t"})
		return s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/call",
			Params: params,
		})
	}()
	if rec != nil {
		t.Fatalf("SECURITY: [gate-panic] panic escaped HandleRequest (%v): on stdio this kills the process; checkToolGate must recover it", rec)
	}
	if resp.Error == nil {
		t.Fatalf("SECURITY: [gate-panic] panicking tool gate came back as a success response: %+v", resp.Result)
	}
	if resp.Error.Code != ErrInternalError {
		t.Fatalf("SECURITY: [gate-panic] error code = %d, want %d (internal error, mirroring checkPromptGate)", resp.Error.Code, ErrInternalError)
	}
	if strings.Contains(resp.Error.Message, panicSecret) {
		t.Fatalf("SECURITY: [gate-panic] panic value leaked to the caller: %q", resp.Error.Message)
	}
	if ran {
		t.Fatal("SECURITY: [gate-panic] tool handler ran behind a panicking gate")
	}
}

// Property: subscriber gates are re-evaluated at DELIVERY time against
// the live predicate, never snapshotted at subscribe time — a principal
// revoked mid-stream stops receiving immediately (sseGetHandler's own
// comment: "a long-lived stream must reflect gate decisions that change
// mid-connection"). The existing gate tests only exercise gates that
// refuse from the start.
func TestGateRevokedMidStreamStopsDelivery(t *testing.T) {
	var revoked atomic.Bool
	s := NewServer()
	s.SetGate(func(ctx context.Context) error {
		if revoked.Load() {
			return errors.New("revoked mid-stream")
		}
		return requireUser(ctx)
	})
	ts := newNotificationServer(t, s)
	events := openStream(t, ts, true)

	// Before revocation the authed stream receives.
	s.NotifyToolsListChanged()
	ev := awaitSSE(t, events, "list_changed before revocation")
	if ev.event != "message" {
		t.Fatalf("event = %q, want message", ev.event)
	}

	// After revocation the same stream goes silent.
	revoked.Store(true)
	s.NotifyToolsListChanged()
	expectNoSSE(t, events, "notification after mid-stream revocation")

	// Item-gate surface: a resources/updated notice gated by the
	// resource's own gate flips from deny to allow mid-stream (and back).
	revoked.Store(false)
	var allowUpdates atomic.Bool
	if err := s.RegisterResource("secret://flip", "Flip", "text/csv",
		func(context.Context) (ResourceContents, error) { return ResourceContents{Text: "x"}, nil },
		WithResourceGate(func(context.Context) error {
			if !allowUpdates.Load() {
				return errors.New("updates denied")
			}
			return nil
		})); err != nil {
		t.Fatal(err)
	}
	// RegisterResource fires a (gate-less, by design) list_changed; drain
	// it so the item-gate assertions below see only resources/updated.
	if ev := awaitSSE(t, events, "list_changed from RegisterResource"); ev.event != "message" {
		t.Fatalf("event = %q, want message", ev.event)
	}
	postMCP(t, ts, true, `{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"secret://flip"}}`)
	s.NotifyResourceUpdated("secret://flip")
	expectNoSSE(t, events, "updated while the item gate denies")
	allowUpdates.Store(true)
	s.NotifyResourceUpdated("secret://flip")
	method, uri := decodeNotification(t, awaitSSE(t, events, "updated after the item gate flips"))
	if method != "notifications/resources/updated" || uri != "secret://flip" {
		t.Fatalf("updated notification = %s %q", method, uri)
	}
}

// Property: the server-wide gate covers every resources method, not just
// the tools and prompts surfaces the earlier tests pinned. A refused
// caller must not list, read, or arm subscriptions.
func TestServerGateClosesResourcesSurface(t *testing.T) {
	s := NewServer()
	if err := staticResource(s, "ui://pub", "Pub", "text/html", "x"); err != nil {
		t.Fatal(err)
	}
	s.SetGate(requireUser)

	cases := []struct {
		method string
		params string
	}{
		{"resources/list", ""},
		{"resources/read", `{"uri":"ui://pub"}`},
		{"resources/subscribe", `{"uri":"ui://pub"}`},
		{"resources/unsubscribe", `{"uri":"ui://pub"}`},
	}
	for _, c := range cases {
		req := Request{JSONRPC: "2.0", ID: 1, Method: c.method}
		if c.params != "" {
			req.Params = json.RawMessage(c.params)
		}
		resp := s.HandleRequest(context.Background(), req)
		if resp.Error == nil {
			t.Errorf("SECURITY: [authz] %s answered a server-gate-refused caller: %+v", c.method, resp)
		}
	}
}

// Property: a recovered gate panic is not silently swallowed. The
// fail-closed contract turns the panic into an internal error, but an
// operator who cannot see the panic cannot tell a gate bug from a
// refusal storm — runCallGate and checkServerGate log the recovered
// value (operation, tool name where available) before answering.
func TestPanickingGatesAreLoggedNotSwallowed(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	if err := runCallGate(func(string) error { panic("call gate boom") }, "kiln_probe"); err == nil || err.Error() != "internal tool error" {
		t.Fatalf("runCallGate: panicking gate must still answer the internal tool error, got %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "call gate boom") {
		t.Errorf("runCallGate: recovered panic not logged: %q", logged)
	}
	if !strings.Contains(logged, "kiln_probe") {
		t.Errorf("runCallGate: log must carry the tool name: %q", logged)
	}
	if !strings.Contains(logged, `operation="call gate"`) {
		t.Errorf("runCallGate: log must carry the structured operation field: %q", logged)
	}

	buf.Reset()
	s := NewServer()
	s.SetGate(func(context.Context) error { panic("server gate boom") })
	if err := s.checkServerGate(context.Background()); err == nil || err.Error() != "internal tool error" {
		t.Fatalf("checkServerGate: panicking gate must still answer the internal tool error, got %v", err)
	}
	logged = buf.String()
	if !strings.Contains(logged, "server gate boom") {
		t.Errorf("checkServerGate: recovered panic not logged: %q", logged)
	}
	if !strings.Contains(logged, `operation="server gate"`) {
		t.Errorf("checkServerGate: log must carry the structured operation field: %q", logged)
	}
}
