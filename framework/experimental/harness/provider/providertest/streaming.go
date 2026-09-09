// Package providertest contains shared checks for provider adapters.
package providertest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// RequestBody is the subset of an OpenAI Chat Completions request used by the
// provider adapter tests. It replaces the identical capturedBody types in the
// openrouter and zai test packages.
type RequestBody struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// DecodeRequestBody reads the request body into RequestBody. It replaces the
// identical decodeBody helpers in the openrouter and zai test packages.
func DecodeRequestBody(t testing.TB, r io.Reader) RequestBody {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var body RequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal request body %q: %v", raw, err)
	}
	return body
}

// StopUsageSSE builds the terminal SSE response used by both provider adapter
// request-shape tests. It replaces their shared scriptedSSE(stopUsageFrame(...))
// setup.
func StopUsageSSE(prompt, completion int) string {
	return scriptedSSE(stopUsageFrame(prompt, completion))
}

// AssertStreamingParse verifies text, stop, usage, and error events from a
// streaming provider. It replaces the identical TestChatStreamingParse bodies
// formerly kept in the openrouter and zai adapter test packages.
func AssertStreamingParse(t testing.TB, newProvider func(baseURL string) provider.Provider) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream") //gofastr:allow(unseated) test-only SSE fixture; no production stream surface
		fmt.Fprint(w, scriptedSSE(
			textFrame("Hel"),
			textFrame("lo"),
			stopUsageFrame(12, 3),
		))
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
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
