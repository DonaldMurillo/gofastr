package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

func sweepCollect(t *testing.T, sse string) []provider.StreamEvent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var out []provider.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestSweepHTTPErrorPreviewIsScrubbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad\x1b[2K\rFORGED‮"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	_, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err == nil {
		t.Fatal("want error on 502")
	}
	if msg := err.Error(); strings.ContainsAny(msg, "\x1b\r‮") || !strings.Contains(msg, "HTTP 502") {
		t.Fatalf("error = %q, want status kept and control/bidi bytes stripped", msg)
	}
}

func TestSweepReasoningIsQuotedThinking(t *testing.T) {
	evs := sweepCollect(t, scriptedSSE(
		`{"choices":[{"delta":{"reasoning_content":"a\"b"}}]}`,
		`{"choices":[{"delta":{"reasoning":"c"}}]}`,
	))
	var got []string
	for _, ev := range evs {
		if ev.Kind == provider.KindThinkingDelta {
			if !json.Valid(ev.Thinking) {
				t.Fatalf("thinking %q is not JSON", ev.Thinking)
			}
			var s string
			_ = json.Unmarshal(ev.Thinking, &s)
			got = append(got, s)
		}
	}
	if strings.Join(got, "|") != `a"b|c` {
		t.Fatalf("thinking deltas = %q, want both reasoning fields as JSON strings", got)
	}
}

func TestSweepUsageTakesLargestCacheField(t *testing.T) {
	evs := sweepCollect(t, scriptedSSE(
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_cache_hit_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":5,"prompt_tokens_details":{"cached_tokens":7}}}`,
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"prompt_cache_hit_tokens":6,"cache_read_input_tokens":4}}`,
	))
	var reads []int
	for _, ev := range evs {
		if ev.Kind == provider.KindUsage {
			reads = append(reads, ev.Usage.CacheReadTokens)
			if ev.Usage.InputTokens != 10 || ev.Usage.OutputTokens != 2 {
				t.Fatalf("usage = %+v", ev.Usage)
			}
		}
	}
	if len(reads) != 2 || reads[0] != 7 || reads[1] != 6 {
		t.Fatalf("cache reads = %v, want [7 6]", reads)
	}
}

func TestSweepFinishClosesOpenToolCallsBeforeStop(t *testing.T) {
	evs := sweepCollect(t, scriptedSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"Echo","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	))
	var kinds []provider.StreamEventKind
	for _, ev := range evs {
		kinds = append(kinds, ev.Kind)
	}
	want := []provider.StreamEventKind{provider.KindToolUseStart, provider.KindToolUseDelta, provider.KindToolUseStop, provider.KindStop, provider.KindStop}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

func TestSweepTemperatureZeroIsOmitted(t *testing.T) {
	body, err := buildBody(&provider.Request{Model: "m", System: "sys"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if strings.Contains(s, "temperature") || strings.Contains(s, "max_tokens") || !strings.Contains(s, `"stream":true`) {
		t.Fatalf("body = %s, want stream true and no temperature/max_tokens for zero values", s)
	}
	if !strings.Contains(s, `"messages":[{"role":"system","content":"sys"}]`) {
		t.Fatalf("body = %s, want the system prompt as the first message", s)
	}
}

func TestSweepUserToolResultsPrecedeText(t *testing.T) {
	out := translateOutbound(provider.Message{Role: provider.RoleUser, Content: []control.ContentBlock{
		{Type: "text", Text: "after"},
		{Type: "tool_result", ToolResult: &control.ToolResultBlk{ToolUseID: "c1", Content: []control.ContentBlock{{Type: "text", Text: "r"}}}},
	}})
	if len(out) != 2 || out[0].Role != "tool" || out[0].Content != "r" || out[1].Role != "user" || out[1].Content != "after" {
		t.Fatalf("translated = %+v, want the tool message first, then the user text", out)
	}
}
