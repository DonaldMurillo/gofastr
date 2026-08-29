package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// mustRegisterPrompt registers a prompt or fails the test.
func mustRegisterPrompt(t *testing.T, s *Server, name string, handler PromptHandler, opts ...PromptOption) {
	t.Helper()
	if err := s.RegisterPrompt(name, handler, opts...); err != nil {
		t.Fatalf("register prompt %s: %v", name, err)
	}
}

// listPromptNames issues prompts/list for the caller ctx identifies and
// returns the visible prompt names (nil on an error response).
func listPromptNames(t *testing.T, s *Server, ctx context.Context) []string {
	t.Helper()
	resp := s.HandleRequest(ctx, Request{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
	if resp.Error != nil {
		return nil
	}
	res, ok := resp.Result.(promptsListResult)
	if !ok {
		t.Fatalf("prompts/list result type %T", resp.Result)
	}
	names := make([]string, 0, len(res.Prompts))
	for _, p := range res.Prompts {
		names = append(names, p.Name)
	}
	return names
}

// callPromptsGet issues prompts/get with raw JSON params.
func callPromptsGet(t *testing.T, s *Server, ctx context.Context, params string) Response {
	t.Helper()
	return s.HandleRequest(ctx, Request{
		JSONRPC: "2.0", ID: 1, Method: "prompts/get",
		Params: json.RawMessage(params),
	})
}

// wireJSON marshals a response result so a test can pin the exact bytes a
// client sees on the wire.
func wireJSON(t *testing.T, resp Response) string {
	t.Helper()
	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return string(b)
}

// TestPromptsListReturnsArguments pins the prompts/list wire shape against
// the MCP spec (2025-06-18): each prompt carries name, optional description,
// and arguments with name/description/required.
func TestPromptsListReturnsArguments(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "code_review",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil },
		WithPromptDescription("Review code"),
		WithPromptArguments(
			PromptArgument{Name: "code", Description: "The code", Required: true},
			PromptArgument{Name: "style", Description: "Review style"},
		))

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "prompts/list"})
	if resp.Error != nil {
		t.Fatalf("prompts/list errored: %v", resp.Error)
	}
	got := wireJSON(t, resp)
	want := `{"prompts":[{"name":"code_review","description":"Review code","arguments":[` +
		`{"name":"code","description":"The code","required":true},` +
		`{"name":"style","description":"Review style"}]}]}`
	if got != want {
		t.Errorf("prompts/list wire shape:\n got %s\nwant %s", got, want)
	}
}

// TestPromptsGetReturnsMessages pins the prompts/get result shape: an
// optional description plus messages of role + content block, per the spec's
// PromptMessage.
func TestPromptsGetReturnsMessages(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "code_review",
		func(_ context.Context, args map[string]string) ([]PromptMessage, error) {
			return []PromptMessage{
				{Role: "user", Content: TextContent("Review this: " + args["code"])},
				{Role: "assistant", Content: TextContent("Ready.")},
			}, nil
		},
		WithPromptDescription("Code review"),
		WithPromptArguments(PromptArgument{Name: "code", Required: true}))

	resp := callPromptsGet(t, s, context.Background(), `{"name":"code_review","arguments":{"code":"x=1"}}`)
	if resp.Error != nil {
		t.Fatalf("prompts/get errored: %v", resp.Error)
	}
	// Content blocks marshal through a map (Content.MarshalJSON), so key
	// order is not pinned; compare the decoded shape instead.
	var got any
	if err := json.Unmarshal([]byte(wireJSON(t, resp)), &got); err != nil {
		t.Fatalf("prompts/get result is not valid JSON: %v", err)
	}
	want := map[string]any{
		"description": "Code review",
		"messages": []any{
			map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "Review this: x=1"}},
			map[string]any{"role": "assistant", "content": map[string]any{"type": "text", "text": "Ready."}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prompts/get wire shape:\n got %s\nwant %v", wireJSON(t, resp), want)
	}
}

// An unknown name is a spec error response (-32602 invalid params), not a
// panic and not silence.
func TestPromptsGetUnknownNameErrors(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "known",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil })

	resp := callPromptsGet(t, s, context.Background(), `{"name":"missing","arguments":{}}`)
	if resp.Error == nil {
		t.Fatal("prompts/get for unknown name returned success")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %d, want %d (invalid params)", resp.Error.Code, ErrInvalidParams)
	}
	if !strings.Contains(resp.Error.Message, "missing") {
		t.Errorf("error message should name the prompt: %q", resp.Error.Message)
	}
	if resp.ID != 1 {
		t.Errorf("error response lost the request id: %v", resp.ID)
	}
}

// A request omitting a declared-required argument is refused before the
// handler runs, the code the spec assigns (invalid params).
func TestPromptsGetMissingRequiredArgErrors(t *testing.T) {
	s := NewServer()
	ran := false
	mustRegisterPrompt(t, s, "code_review",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			ran = true
			return nil, nil
		},
		WithPromptArguments(PromptArgument{Name: "code", Required: true}))

	resp := callPromptsGet(t, s, context.Background(), `{"name":"code_review","arguments":{}}`)
	if resp.Error == nil {
		t.Fatal("prompts/get without the required argument succeeded")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %d, want %d (invalid params)", resp.Error.Code, ErrInvalidParams)
	}
	if ran {
		t.Error("handler ran despite missing required argument")
	}
	if resp := callPromptsGet(t, s, context.Background(), `{"name":"code_review","arguments":{"code":"x"}}`); resp.Error != nil {
		t.Fatalf("prompts/get with the required argument errored: %v", resp.Error)
	}
}

// A WithPromptGate refusal blocks prompts/get: the handler never runs.
func TestPromptGateRefusesGet(t *testing.T) {
	s := NewServer()
	ran := false
	mustRegisterPrompt(t, s, "secret_prompt",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			ran = true
			return nil, nil
		},
		WithPromptGate(requireUser))

	resp := callPromptsGet(t, s, context.Background(), `{"name":"secret_prompt"}`)
	if resp.Error == nil {
		t.Fatal("SECURITY: [authz] gated prompt ran for an unauthenticated caller")
	}
	if ran {
		t.Error("handler ran despite gate refusal")
	}
	if resp := callPromptsGet(t, s, authed(context.Background()), `{"name":"secret_prompt"}`); resp.Error != nil {
		t.Errorf("gate refused an authenticated caller: %v", resp.Error)
	}
}

// A panic in a prompt handler (or its gate) is a well-formed internal
// error, never a transport crash: the prompts/get analogue of the
// readResourceContents recover guard.
func TestPromptsGetPanicBecomesError(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "boom",
		func(context.Context, map[string]string) ([]PromptMessage, error) { panic("kaboom") })
	mustRegisterPrompt(t, s, "boom_gate",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil },
		WithPromptGate(func(context.Context) error { panic("gate boom") }))

	for _, name := range []string{"boom", "boom_gate"} {
		resp := callPromptsGet(t, s, context.Background(), `{"name":"`+name+`"}`)
		if resp.Error == nil {
			t.Errorf("%s: panic became a success response", name)
			continue
		}
		if resp.Error.Code != ErrInternalError {
			t.Errorf("%s: error code = %d, want %d (internal)", name, resp.Error.Code, ErrInternalError)
		}
		if resp.Error.Message != "internal prompt error" {
			t.Errorf("%s: panic value leaked to the caller: %q", name, resp.Error.Message)
		}
	}
}

// The prompts capability appears only when a prompt is registered, matching
// the resources capability contract.
func TestInitializeAdvertisesPrompts(t *testing.T) {
	s := NewServer()
	mustRegisterPrompt(t, s, "p",
		func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil })

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize errored: %v", resp.Error)
	}
	blob := wireJSON(t, resp)
	if !strings.Contains(blob, `"prompts"`) {
		t.Errorf("initialize did not advertise the prompts capability: %s", blob)
	}
}

func TestInitializeOmitsPromptsWhenEmpty(t *testing.T) {
	s := NewServer()
	// Other registries populated, prompts empty: the capability must still
	// stay absent.
	_ = s.RegisterResource("ui://x", "X", "text/html",
		func(context.Context) (ResourceContents, error) { return ResourceContents{}, nil })

	resp := s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize errored: %v", resp.Error)
	}
	blob := wireJSON(t, resp)
	if strings.Contains(blob, `"prompts"`) {
		t.Errorf("empty prompt registry advertised the prompts capability: %s", blob)
	}
}

func TestRegisterPrompt_Validation(t *testing.T) {
	s := NewServer()
	h := func(context.Context, map[string]string) ([]PromptMessage, error) { return nil, nil }
	if err := s.RegisterPrompt("", h); err == nil {
		t.Error("empty name should error")
	}
	if err := s.RegisterPrompt("p", nil); err == nil {
		t.Error("nil handler should error")
	}
	if err := s.RegisterPrompt("p", h); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterPrompt("p", h); err == nil {
		t.Error("duplicate name should error")
	}
}
