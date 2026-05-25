package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// scriptedSSE writes one SSE "data: " line per element, then [DONE].
func scriptedSSE(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: ")
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestChatTextOnly(t *testing.T) {
	chunk := func(text string) string {
		c := streamChunk{Choices: []streamChoice{{}}}
		c.Choices[0].Delta.Content = text
		b, _ := json.Marshal(c)
		return string(b)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("missing Authorization: %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, scriptedSSE(chunk("Hel"), chunk("lo")))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	var stops int
	for ev := range ch {
		switch ev.Kind {
		case provider.KindTextDelta:
			got.WriteString(ev.Text)
		case provider.KindStop:
			stops++
		}
	}
	if got.String() != "Hello" {
		t.Errorf("text = %q, want Hello", got.String())
	}
	if stops < 1 {
		t.Errorf("expected at least one Stop event")
	}
}

func TestChatToolUseStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Two delta chunks for the same tool call: first carries
		// the name + first arg fragment, second carries the rest.
		c1 := streamChunk{Choices: []streamChoice{{Delta: struct {
			Content          string          `json:"content,omitempty"`
			ToolCalls        []deltaToolCall `json:"tool_calls,omitempty"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			Reasoning        string          `json:"reasoning,omitempty"`
		}{ToolCalls: []deltaToolCall{{
			Index: 0,
			ID:    "call_x",
			Function: &deltaToolCallFunc{
				Name:      "Echo",
				Arguments: `{"text":`,
			},
		}}}}}}
		c2 := streamChunk{Choices: []streamChoice{{Delta: struct {
			Content          string          `json:"content,omitempty"`
			ToolCalls        []deltaToolCall `json:"tool_calls,omitempty"`
			ReasoningContent string          `json:"reasoning_content,omitempty"`
			Reasoning        string          `json:"reasoning,omitempty"`
		}{ToolCalls: []deltaToolCall{{
			Index:    0,
			Function: &deltaToolCallFunc{Arguments: `"hi"}`},
		}}}, FinishReason: stringPtr("tool_calls")}}}
		b1, _ := json.Marshal(c1)
		b2, _ := json.Marshal(c2)
		fmt.Fprint(w, scriptedSSE(string(b1), string(b2)))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, _ := c.Chat(context.Background(), &provider.Request{Model: "m"})
	var (
		gotStart  bool
		fragments []string
	)
	for ev := range ch {
		switch ev.Kind {
		case provider.KindToolUseStart:
			gotStart = true
		case provider.KindToolUseDelta:
			fragments = append(fragments, ev.InputDelta)
		}
	}
	if !gotStart {
		t.Error("missing ToolUseStart")
	}
	if got := strings.Join(fragments, ""); got != `{"text":"hi"}` {
		t.Errorf("input deltas concat = %q, want {\"text\":\"hi\"}", got)
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid key"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "bad", Name: "test"}
	_, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestTranslateOutboundAssistantToolUse(t *testing.T) {
	msg := provider.Message{
		Role: provider.RoleAssistant,
		Content: []control.ContentBlock{
			{Type: "text", Text: "Calling tool"},
			{Type: "tool_use", ToolUse: &control.ToolUse{
				ID:    "call_xyz",
				Name:  "Read",
				Input: json.RawMessage(`{"path":"x"}`),
			}},
		},
	}
	out := translateOutbound(msg)
	if len(out) != 1 {
		t.Fatalf("messages = %d, want 1", len(out))
	}
	if out[0].Role != "assistant" {
		t.Errorf("role = %q", out[0].Role)
	}
	if len(out[0].ToolCalls) != 1 || out[0].ToolCalls[0].Function.Name != "Read" {
		t.Errorf("missing tool_call: %+v", out[0])
	}
}

// TestTranslateOutboundAssistantOnlyToolCalls covers the bug where
// an assistant message with tool_calls but no text emitted
// `"content": ""` on the wire — some OpenAI-compatible providers
// (notably ZAI GLM) silently returned an empty next-turn response
// when they saw an empty string instead of an omitted/null content.
// Regression for the broken-session bug in sess_01KSC42A5VY*.
func TestTranslateOutboundAssistantOnlyToolCalls(t *testing.T) {
	msg := provider.Message{
		Role: provider.RoleAssistant,
		Content: []control.ContentBlock{
			{Type: "tool_use", ToolUse: &control.ToolUse{
				ID:    "call_xyz",
				Name:  "WebFetch",
				Input: json.RawMessage(`{"url":"x"}`),
			}},
		},
	}
	out := translateOutbound(msg)
	if len(out) != 1 {
		t.Fatalf("messages = %d, want 1", len(out))
	}
	// Critical: Content must be nil so json omitempty drops it.
	if out[0].Content != nil {
		t.Errorf("assistant w/ only tool_calls leaked content field: %#v (want nil so omitempty skips it)",
			out[0].Content)
	}
	// Confirm it serializes WITHOUT a content field.
	raw, err := json.Marshal(out[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"content"`) {
		t.Errorf("wire JSON includes content key when there are tool_calls but no text: %s", raw)
	}
}

func TestTranslateOutboundUserToolResult(t *testing.T) {
	msg := provider.Message{
		Role: provider.RoleUser,
		Content: []control.ContentBlock{
			{Type: "tool_result", ToolResult: &control.ToolResultBlk{
				ToolUseID: "call_xyz",
				Content:   []control.ContentBlock{{Type: "text", Text: "file content"}},
			}},
		},
	}
	out := translateOutbound(msg)
	if len(out) != 1 || out[0].Role != "tool" || out[0].ToolCallID != "call_xyz" {
		t.Fatalf("translated = %+v", out)
	}
}

func stringPtr(s string) *string { return &s }
