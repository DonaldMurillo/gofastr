package mcp

// Pins that every params decode in core/mcp resolved duplicated and
// case-folded keys silently last-wins, found by the 2026-09-04
// red-probe round; fixed in protocol.go by running req.Params through
// handler.CheckObjectKeys at the HandleRequest dispatch chokepoint,
// which every params-decoding method sits behind.
// Family: F6 (message-shape ambiguity: executor/first-occurrence-parser disagreement)
// Property: a duplicated or case-folded key inside a method's params object is
// refused with invalid-params at every JSON-RPC method that decodes params; the
// executor must never resolve it silently last-wins.
// Surfaces: protocol.go:HandleRequest (the chokepoint), covering
// tools.go:handleToolsCall, prompts.go:handlePromptsGet (and
// handlePromptsList), resources.go:handleResourcesRead (and
// handleResourcesList, resource_templates.go), notifications.go:
// handleResourcesSubscribe, handleResourcesUnsubscribe, cursor.go:
// listOffset (tools/list et al).

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// recPrompt is a prompt handler that records it ran.
func recPrompt(ran *bool) PromptHandler {
	return func(_ context.Context, _ map[string]string) ([]PromptMessage, error) {
		*ran = true
		return nil, nil
	}
}

// regStatic registers one static resource that records reads.
func regStatic(t *testing.T, s *Server, uri string, ran *bool) {
	t.Helper()
	err := s.RegisterResource(uri, "res-"+uri, "text/plain", func(context.Context) (ResourceContents, error) {
		*ran = true
		return ResourceContents{Text: "body of " + uri}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// expectRefusedParams fails unless resp is an invalid-params error.
func expectRefusedParams(t *testing.T, surface, rawParams string, resp Response) {
	t.Helper()
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Errorf("SECURITY: [param-ambiguity] %s accepted params %s (resp=%+v); a duplicated or case-folded key must be refused with invalid-params, never resolved last-wins",
			surface, rawParams, resp)
	}
}

// callRawParams drives one HandleRequest with raw params bytes.
func callRawParams(s *Server, method string, rawParams string) Response {
	return s.HandleRequest(context.Background(), Request{
		JSONRPC: "2.0", ID: 1, Method: method, Params: json.RawMessage(rawParams),
	})
}

// TestParamsDuplicateKeysRefused: one property (no duplicate/case-folded key
// inside params may resolve last-wins) asserted at every surface that decodes
// params. Three attack shapes total: exact duplicate, case-folded duplicate,
// and the same ambiguity on the paging cursor.
func TestParamsDuplicateKeysRefused(t *testing.T) {
	t.Run("tools/call duplicate name", func(t *testing.T) {
		s := NewServer()
		targetRan := false
		for _, name := range []string{"t_open", "t_target"} {
			n := name
			if err := s.RegisterTool(n, "d", map[string]any{"type": "object"},
				func(_ context.Context, _ map[string]any) (any, error) {
					if n == "t_target" {
						targetRan = true
					}
					return "ok", nil
				}); err != nil {
				t.Fatal(err)
			}
		}
		raw := `{"name":"t_open","name":"t_target","arguments":{}}`
		resp := callRawParams(s, "tools/call", raw)
		expectRefusedParams(t, "tools/call", raw, resp)
		if targetRan {
			t.Errorf("SECURITY: [param-ambiguity] tools/call executed the SECOND duplicate name t_target; a first-occurrence parser saw t_open")
		}
	})

	t.Run("tools/call case-folded name", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("t_open", "d", map[string]any{"type": "object"},
			func(context.Context, map[string]any) (any, error) { return "ok", nil }); err != nil {
			t.Fatal(err)
		}
		// "Name" case-folds onto the same struct field (stdlib json matches
		// tags case-insensitively), so this is a duplicate modulo folding.
		raw := `{"name":"t_open","Name":"t_open"}`
		resp := callRawParams(s, "tools/call", raw)
		expectRefusedParams(t, "tools/call", raw, resp)
	})

	t.Run("prompts/get duplicate name", func(t *testing.T) {
		s := NewServer()
		openRan, targetRan := false, false
		if err := s.RegisterPrompt("p_open", recPrompt(&openRan)); err != nil {
			t.Fatal(err)
		}
		if err := s.RegisterPrompt("p_target", recPrompt(&targetRan)); err != nil {
			t.Fatal(err)
		}
		raw := `{"name":"p_open","name":"p_target"}`
		resp := callRawParams(s, "prompts/get", raw)
		expectRefusedParams(t, "prompts/get", raw, resp)
		if targetRan {
			t.Errorf("SECURITY: [param-ambiguity] prompts/get executed the SECOND duplicate prompt p_target")
		}
	})

	t.Run("resources/read duplicate uri", func(t *testing.T) {
		s := NewServer()
		openRan, secretRan := false, false
		regStatic(t, s, "ui://open/x", &openRan)
		regStatic(t, s, "ui://secret/x", &secretRan)
		raw := `{"uri":"ui://open/x","uri":"ui://secret/x"}`
		resp := callRawParams(s, "resources/read", raw)
		expectRefusedParams(t, "resources/read", raw, resp)
		if secretRan {
			t.Errorf("SECURITY: [param-ambiguity] resources/read read the SECOND duplicate uri ui://secret/x")
		}
	})

	t.Run("resources/subscribe duplicate uri", func(t *testing.T) {
		s := NewServer()
		openRan, secretRan := false, false
		regStatic(t, s, "ui://open/x", &openRan)
		regStatic(t, s, "ui://secret/x", &secretRan)
		raw := `{"uri":"ui://open/x","uri":"ui://secret/x"}`
		resp := callRawParams(s, "resources/subscribe", raw)
		expectRefusedParams(t, "resources/subscribe", raw, resp)
	})

	t.Run("resources/unsubscribe duplicate uri", func(t *testing.T) {
		s := NewServer()
		openRan, secretRan := false, false
		regStatic(t, s, "ui://open/x", &openRan)
		regStatic(t, s, "ui://secret/x", &secretRan)
		raw := `{"uri":"ui://open/x","uri":"ui://secret/x"}`
		resp := callRawParams(s, "resources/unsubscribe", raw)
		expectRefusedParams(t, "resources/unsubscribe", raw, resp)
	})

	t.Run("tools/list duplicate cursor", func(t *testing.T) {
		s := NewServer()
		// More tools than one page so a page-2 cursor exists.
		for i := range defaultListPageSize + 5 {
			name := fmt.Sprintf("t_%03d", i)
			if err := s.RegisterTool(name, "d", map[string]any{"type": "object"},
				func(context.Context, map[string]any) (any, error) { return "ok", nil }); err != nil {
				t.Fatal(err)
			}
		}
		first := s.HandleRequest(context.Background(), Request{
			JSONRPC: "2.0", ID: 1, Method: "tools/list",
			Params: json.RawMessage(`{}`),
		})
		var res struct {
			NextCursor string
		}
		if lr, ok := first.Result.(toolsListResult); ok {
			res.NextCursor = lr.NextCursor
		}
		if res.NextCursor == "" {
			t.Fatal("expected a page-2 cursor from the first tools/list page; page size assumption broke")
		}
		// First occurrence is garbage (a first-occurrence parser refuses it),
		// the last is the legitimate cursor the executor walks.
		raw := fmt.Sprintf(`{"cursor":"forged","cursor":%q}`, res.NextCursor)
		resp := callRawParams(s, "tools/list", raw)
		expectRefusedParams(t, "tools/list", raw, resp)
	})

	// Sibling surfaces behind the same chokepoint: every other
	// params-decoding method, including the ones whose params are only
	// read for the optional cursor and initialize, whose params the
	// dispatcher never reads at all.
	t.Run("every method refuses duplicate params", func(t *testing.T) {
		s := NewServer()
		if err := s.RegisterTool("t", "d", map[string]any{"type": "object"},
			func(context.Context, map[string]any) (any, error) { return "ok", nil }); err != nil {
			t.Fatal(err)
		}
		raw := `{"cursor":"a","cursor":"b"}`
		for _, method := range []string{
			"tools/list", "resources/list", "resources/templates/list",
			"prompts/list", "initialize",
		} {
			expectRefusedParams(t, method, raw, callRawParams(s, method, raw))
		}
	})
}
