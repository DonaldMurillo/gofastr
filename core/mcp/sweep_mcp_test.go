package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sweepCall(t *testing.T, s *Server, method string, params any) Response {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: method, Params: raw})
}

func sweepQuiet(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestSweepPanickingGatesMaskAsUnknown(t *testing.T) {
	sweepQuiet(t)
	boom := func(context.Context) error { panic("boom") }
	noop := func(context.Context, map[string]any) (any, error) { return "ran", nil }
	s := NewServer()
	if err := s.RegisterTool("listed", "", nil, noop, WithToolGate(boom)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterTool("wrapped", "", nil, Gated(boom, noop)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterPrompt("p", func(context.Context, map[string]string) ([]PromptMessage, error) {
		return nil, nil
	}, WithPromptGate(boom)); err != nil {
		t.Fatal(err)
	}

	list := sweepCall(t, s, "tools/list", map[string]any{})
	if res, ok := list.Result.(toolsListResult); !ok || len(res.Tools) != 1 || res.Tools[0].Name != "wrapped" {
		t.Fatalf("a panicking WithToolGate must hide its tool from tools/list, got %+v", list.Result)
	}
	call := sweepCall(t, s, "tools/call", map[string]any{"name": "wrapped"})
	if call.Error == nil || call.Error.Code != ErrMethodNotFound || call.Error.Message != `tool "wrapped" not found` {
		t.Fatalf("a panicking Gated gate must answer as an unknown tool, got %+v", call.Error)
	}
	inner := Gated(boom, noop)
	if err := s.RegisterTool("closure", "", nil, func(ctx context.Context, p map[string]any) (any, error) { return inner(ctx, p) }); err != nil {
		t.Fatal(err)
	}
	call = sweepCall(t, s, "tools/call", map[string]any{"name": "closure"})
	if call.Error == nil || call.Error.Code != ErrMethodNotFound || call.Error.Message != `tool "closure" not found` {
		t.Fatalf("a panicking Gated gate inside a closure must answer as an unknown tool, got %+v", call.Error)
	}
	var get Response
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("a panicking prompt gate escaped prompts/get: %v", rec)
			}
		}()
		get = sweepCall(t, s, "prompts/get", map[string]any{"name": "p"})
	}()
	if get.Error == nil || get.Error.Code != ErrInvalidParams || get.Error.Message != `prompt "p" not found` {
		t.Fatalf("a panicking prompt gate must answer as an unknown prompt, got %+v", get.Error)
	}
}

func TestSweepDevImpliedToolsWithdrawnAndBarred(t *testing.T) {
	sweepQuiet(t)
	noop := func(context.Context, map[string]any) (any, error) { return nil, nil }
	s := NewServer()
	for _, name := range []string{"b-dev", "a-dev"} {
		if err := s.RegisterTool(name, "", nil, noop, WithDevImplied()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RegisterTool("explicit", "", nil, noop); err != nil {
		t.Fatal(err)
	}
	got := s.UnregisterDevImpliedTools()
	if len(got) != 2 || got[0] != "a-dev" || got[1] != "b-dev" {
		t.Fatalf("withdrawn = %v, want [a-dev b-dev]", got)
	}
	if !s.HasTool("explicit") || s.HasTool("a-dev") {
		t.Fatal("only the dev-implied tools may be withdrawn")
	}
	if err := s.RegisterTool("late-dev", "", nil, noop, WithDevImplied()); err != nil {
		t.Fatalf("a barred dev-implied registration must not fail boot: %v", err)
	}
	if s.HasTool("late-dev") {
		t.Fatal("a dev-implied tool registered after the withdrawal was served")
	}
	if err := s.RegisterTool("late-explicit", "", nil, noop); err != nil || !s.HasTool("late-explicit") {
		t.Fatalf("an explicit tool after the withdrawal must register: %v", err)
	}
}

func TestSweepHugeIntegerArgumentKeepsDigits(t *testing.T) {
	var got map[string]any
	s := NewServer()
	if err := s.RegisterTool("t", "", nil, func(_ context.Context, p map[string]any) (any, error) {
		got = p
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"name":"t","arguments":{"big":9007199254740993,"small":7,"frac":1.5}}`)
	if resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: raw}); resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if n, ok := got["big"].(json.Number); !ok || n.String() != "9007199254740993" {
		t.Errorf("big = %#v, want json.Number 9007199254740993", got["big"])
	}
	if f, ok := got["small"].(float64); !ok || f != 7 {
		t.Errorf("small = %#v, want float64 7", got["small"])
	}
	if f, ok := got["frac"].(float64); !ok || f != 1.5 {
		t.Errorf("frac = %#v, want float64 1.5", got["frac"])
	}
}

func TestSweepDistinctURICapAppliesAcrossCallers(t *testing.T) {
	s := NewServer()
	subscribe := func(caller, i int, uri string) Response {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = fmt.Sprintf("10.0.0.%d:4000", caller+1)
		return s.HandleRequest(WithRequest(context.Background(), r), Request{
			JSONRPC: "2.0", ID: i, Method: "resources/subscribe",
			Params: json.RawMessage(`{"uri":"` + uri + `"}`),
		})
	}
	for i := range maxResourceSubscriptions {
		if resp := subscribe(i%4, i, fmt.Sprintf("x://%d", i)); resp.Error != nil {
			t.Fatalf("subscribe %d under the cap failed: %v", i, resp.Error)
		}
	}
	if resp := subscribe(9, 0, "x://fresh-caller"); resp.Error == nil {
		t.Fatal("a caller with no subscriptions grew the map past the distinct-uri cap")
	}
	if resp := subscribe(9, 0, "x://5"); resp.Error != nil {
		t.Fatalf("subscribing to an already-held uri at the cap was refused: %v", resp.Error)
	}
}

func TestSweepDefaultSeatCapIsSixteen(t *testing.T) {
	s := NewServer()
	for i := range 16 {
		if _, ok := s.addSSESubscriber(context.Background(), "peer:10.0.0.1"); !ok {
			t.Fatalf("stream %d refused under the default cap", i+1)
		}
	}
	if _, ok := s.addSSESubscriber(context.Background(), "peer:10.0.0.1"); ok {
		t.Fatal("a 17th stream was admitted under the default cap of 16")
	}
}
