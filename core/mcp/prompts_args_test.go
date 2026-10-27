package mcp

import (
	"context"
	"testing"
)

// Prompt arguments are strings. A null decoded into a map[string]string as
// "", so a required argument sent as null passed the presence check and
// reached the handler empty.
func TestPromptArgNullRefused(t *testing.T) {
	s := NewServer()
	ran := false
	mustRegisterPrompt(t, s, "code_review",
		func(context.Context, map[string]string) ([]PromptMessage, error) {
			ran = true
			return nil, nil
		},
		WithPromptArguments(
			PromptArgument{Name: "code", Required: true},
			PromptArgument{Name: "style"},
		))
	for _, args := range []string{
		`{"code":null}`,
		`{"code":"x","style":null}`,
		`{"code":5}`,
		`{"code":["x"]}`,
	} {
		ran = false
		resp := callPromptsGet(t, s, context.Background(), `{"name":"code_review","arguments":`+args+`}`)
		if resp.Error == nil || resp.Error.Code != ErrInvalidParams || ran {
			t.Errorf("arguments %s: error %v ran %v, want invalid params before the handler", args, resp.Error, ran)
		}
	}
	if resp := callPromptsGet(t, s, context.Background(), `{"name":"code_review","arguments":{"code":""}}`); resp.Error != nil {
		t.Errorf("an empty string is a string: %v", resp.Error)
	}
}
