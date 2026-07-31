package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// scriptedSSE writes one SSE "data: " line per element, then [DONE].
// Mirrors internal/openai/client_test.go's scriptedSSE exactly.
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

// textFrame builds a text-delta SSE frame in the OpenAI Chat
// Completions streaming shape the internal parser expects. We can't
// reference the unexported streamChunk/streamChoice types from this
// package, so we mirror their JSON tags with local structs.
func textFrame(s string) string {
	type delta struct {
		Content string `json:"content,omitempty"`
	}
	type choice struct {
		Delta delta `json:"delta"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
	}
	b, _ := json.Marshal(chunk{Choices: []choice{{Delta: delta{Content: s}}}})
	return string(b)
}

// stopUsageFrame builds the terminal frame carrying a finish_reason
// and a usage block, exercising both the KindStop and KindUsage
// emission paths in the SSE parser.
func stopUsageFrame(prompt, completion int) string {
	type delta struct{}
	type choice struct {
		Delta        delta   `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	}
	type usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
		Usage   *usage   `json:"usage,omitempty"`
	}
	fr := "stop"
	b, _ := json.Marshal(chunk{
		Choices: []choice{{FinishReason: &fr}},
		Usage:   &usage{PromptTokens: prompt, CompletionTokens: completion},
	})
	return string(b)
}

// capturedBody is the subset of the OpenAI Chat Completions request
// body these tests assert on.
type capturedBody struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func decodeBody(t *testing.T, r io.Reader) capturedBody {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var body capturedBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal request body %q: %v", raw, err)
	}
	return body
}

// TestChatRequestShape asserts the outbound request carries the
// bearer token, the Request.Model on the wire, the system prompt as
// the first OpenAI message, and the user text block as a user
// message. OpenRouter additionally sends HTTP-Referer and X-Title.
func TestChatRequestShape(t *testing.T) {
	var gotAuth, gotReferer, gotTitle string
	var body capturedBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		body = decodeBody(t, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, scriptedSSE(stopUsageFrame(1, 1)))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "sk-test", BaseURL: srv.URL}
	ch, err := p.Chat(context.Background(), &provider.Request{
		Model:  "m",
		System: "sys",
		Messages: []provider.Message{{
			Role:    provider.RoleUser,
			Content: []control.ContentBlock{{Type: "text", Text: "hello world"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if want := "Bearer sk-test"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotReferer == "" {
		t.Errorf("HTTP-Referer header missing; OpenRouter requires it")
	}
	if gotTitle == "" {
		t.Errorf("X-Title header missing; OpenRouter requires it")
	}
	if body.Model != "m" {
		t.Errorf("body.model = %q, want %q", body.Model, "m")
	}
	if !body.Stream {
		t.Errorf("body.stream = false, want true (streaming request)")
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %+v", len(body.Messages), body.Messages)
	}
	if body.Messages[0].Role != "system" || body.Messages[0].Content != "sys" {
		t.Errorf("messages[0] = {role:%q content:%q}, want {system sys}",
			body.Messages[0].Role, body.Messages[0].Content)
	}
	if body.Messages[1].Role != "user" || body.Messages[1].Content != "hello world" {
		t.Errorf("messages[1] = {role:%q content:%q}, want {user hello world}",
			body.Messages[1].Role, body.Messages[1].Content)
	}
}

// TestChatStreamingParse drains the event channel and asserts at
// least one KindTextDelta with the streamed text plus a terminal
// KindStop and a KindUsage carrying token counts.
func TestChatStreamingParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, scriptedSSE(
			textFrame("Hel"),
			textFrame("lo"),
			stopUsageFrame(12, 3),
		))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "sk-test", BaseURL: srv.URL}
	ch, err := p.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}

	var (
		text     strings.Builder
		deltas   int
		stops    int
		usage    *provider.Usage
		hadError bool
	)
	for ev := range ch {
		switch ev.Kind {
		case provider.KindTextDelta:
			deltas++
			text.WriteString(ev.Text)
		case provider.KindStop:
			stops++
		case provider.KindUsage:
			usage = ev.Usage
		case provider.KindError:
			hadError = true
			t.Errorf("unexpected KindError: %v", ev.Err)
		}
	}
	if hadError {
		t.FailNow()
	}
	if deltas < 1 {
		t.Errorf("expected at least one KindTextDelta, got %d", deltas)
	}
	if got, want := text.String(), "Hello"; got != want {
		t.Errorf("concatenated deltas = %q, want %q", got, want)
	}
	if stops < 1 {
		t.Errorf("expected at least one KindStop terminal event, got %d", stops)
	}
	if usage == nil {
		t.Fatalf("missing KindUsage event")
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 3 {
		t.Errorf("usage = {in:%d out:%d}, want {in:12 out:3}",
			usage.InputTokens, usage.OutputTokens)
	}
}

// TestChatHTTPError401 asserts a 401 response surfaces as an error
// whose message contains "401" (mirrors client_test.go).
func TestChatHTTPError401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid key"))
	}))
	defer srv.Close()

	p := &Provider{APIKey: "bad", BaseURL: srv.URL}
	_, err := p.Chat(context.Background(), &provider.Request{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want error containing 401", err)
	}
}
