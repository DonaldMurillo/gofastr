package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// Client speaks the OpenAI Chat Completions wire format
// (`/chat/completions` with the OpenAI-style `messages` array and SSE
// streaming). OpenRouter and ZAI both speak this; the only
// differences are the base URL, the auth header, and any required
// metadata headers (HTTP-Referer / X-Title for OpenRouter).
type Client struct {
	BaseURL string         // e.g. "https://openrouter.ai/api/v1"
	APIKey  string         // bearer token
	HTTP    *http.Client   // nil → http.DefaultClient with a 5-min timeout
	Headers map[string]string // additional static headers (e.g. HTTP-Referer)
	Name    string         // provider name for events ("openrouter", "zai")
}

// Chat issues a Chat Completions request and streams events.
func (c *Client) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	body, err := buildBody(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("%s: HTTP %d: %s", c.Name, resp.StatusCode, string(raw))
	}

	ch := make(chan provider.StreamEvent, 32)
	go parseSSEStream(resp.Body, ch)
	return ch, nil
}

// requestBody is the OpenAI-shape JSON. Only fields the harness emits are listed.
type requestBody struct {
	Model       string         `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int           `json:"max_tokens,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
}

type openaiMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // "function"
	Function openaiToolCallFn   `json:"function"`
}

type openaiToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiTool struct {
	Type     string             `json:"type"` // "function"
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func buildBody(req *provider.Request) ([]byte, error) {
	out := requestBody{
		Model:  req.Model,
		Stream: true,
	}
	if req.Temperature > 0 {
		t := req.Temperature
		out.Temperature = &t
	}
	if req.MaxTokens > 0 {
		out.MaxTokens = &req.MaxTokens
	}
	// System message first.
	if req.System != "" {
		out.Messages = append(out.Messages, openaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, translateOutbound(m)...)
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, openaiTool{
			Type: "function",
			Function: openaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return json.Marshal(out)
}

// translateOutbound converts one canonical Message into one or more
// OpenAI-shape messages. The expansion is necessary because OpenAI
// represents a turn with tool calls as separate messages: assistant
// emits a message with tool_calls; the next user message provides
// tool_result-shaped entries with role=tool.
func translateOutbound(m provider.Message) []openaiMessage {
	switch m.Role {
	case provider.RoleAssistant:
		// Split text + tool_use blocks.
		var text strings.Builder
		var toolCalls []openaiToolCall
		for _, b := range m.Content {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				if b.ToolUse != nil {
					toolCalls = append(toolCalls, openaiToolCall{
						ID:   b.ToolUse.ID,
						Type: "function",
						Function: openaiToolCallFn{
							Name:      b.ToolUse.Name,
							Arguments: string(b.ToolUse.Input),
						},
					})
				}
			}
		}
		// OpenAI-compatible spec: when an assistant message carries
		// tool_calls, content should be null (omitted) — emitting
		// `"content": ""` confuses some providers (notably ZAI GLM,
		// which silently returns an empty next-turn response). Pass
		// `any(nil)` so omitempty actually skips the field.
		var contentField any
		if t := text.String(); t != "" {
			contentField = t
		} else if len(toolCalls) == 0 {
			// No text AND no tool_calls — still need to emit an empty
			// assistant message rather than nothing, so keep "".
			contentField = ""
		}
		return []openaiMessage{{
			Role:      "assistant",
			Content:   contentField,
			ToolCalls: toolCalls,
		}}
	case provider.RoleUser:
		// User messages that contain tool_result blocks expand into
		// one openaiMessage{role=tool} per tool_result, plus the text
		// blocks as a user message at the end. OpenAI's wire requires
		// tool messages immediately follow the assistant tool_calls.
		var out []openaiMessage
		var text strings.Builder
		for _, b := range m.Content {
			switch b.Type {
			case "tool_result":
				if b.ToolResult != nil {
					content := flattenToolResultContent(b.ToolResult.Content)
					out = append(out, openaiMessage{
						Role:       "tool",
						Content:    content,
						ToolCallID: b.ToolResult.ToolUseID,
					})
				}
			case "text":
				text.WriteString(b.Text)
			}
		}
		if text.Len() > 0 {
			out = append(out, openaiMessage{Role: "user", Content: text.String()})
		}
		return out
	case provider.RoleSystem:
		// Already prepended in buildBody from req.System; ignore extras.
		return nil
	}
	return nil
}

func flattenToolResultContent(blocks []control.ContentBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// --- Streaming response parser ---

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *struct {
		PromptTokens             int `json:"prompt_tokens"`
		CompletionTokens         int `json:"completion_tokens"`
		// Provider-specific cache fields. OpenRouter passes
		// through cache details from upstream models when
		// available; OpenAI returns prompt_tokens_details.
		PromptCacheHit           int `json:"prompt_cache_hit_tokens,omitempty"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
		PromptTokensDetails      *struct {
			CachedTokens int `json:"cached_tokens,omitempty"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}

type streamChoice struct {
	Delta struct {
		Content   string          `json:"content,omitempty"`
		ToolCalls []deltaToolCall `json:"tool_calls,omitempty"`
		// ReasoningContent is where ZAI GLM and DeepSeek surface
		// thinking-block text on the OpenAI-compatible wire. Some
		// providers use `reasoning` instead — accept both.
		ReasoningContent string `json:"reasoning_content,omitempty"`
		Reasoning        string `json:"reasoning,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type deltaToolCall struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id,omitempty"`
	Function *deltaToolCallFunc  `json:"function,omitempty"`
}

type deltaToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func parseSSEStream(body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer body.Close()
	defer close(ch)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// Per-stream state: track active tool_call slots by index so
	// argument deltas accumulate to the right call.
	activeToolCalls := map[int]*control.ToolUse{}
	emittedStarts := map[int]bool{}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
			return
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			ch <- provider.StreamEvent{Kind: provider.KindError, Err: fmt.Errorf("parse chunk: %w", err)}
			return
		}
		for _, c := range chunk.Choices {
			// Reasoning content (GLM, DeepSeek) flows on its own
			// field before the regular content starts. Quote it as
			// a JSON string so the envelope's RawMessage stays
			// well-formed when the engine re-emits it on the bus.
			if reasoning := c.Delta.ReasoningContent + c.Delta.Reasoning; reasoning != "" {
				if quoted, err := json.Marshal(reasoning); err == nil {
					ch <- provider.StreamEvent{Kind: provider.KindThinkingDelta, Thinking: quoted}
				}
			}
			if c.Delta.Content != "" {
				ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: c.Delta.Content}
			}
			for _, tc := range c.Delta.ToolCalls {
				if _, exists := activeToolCalls[tc.Index]; !exists {
					name := ""
					id := tc.ID
					if tc.Function != nil {
						name = tc.Function.Name
					}
					activeToolCalls[tc.Index] = &control.ToolUse{ID: id, Name: name}
				} else if tc.ID != "" {
					activeToolCalls[tc.Index].ID = tc.ID
				}
				active := activeToolCalls[tc.Index]
				if tc.Function != nil && tc.Function.Name != "" && active.Name == "" {
					active.Name = tc.Function.Name
				}
				// Emit Start once we have a name.
				if !emittedStarts[tc.Index] && active.Name != "" {
					ch <- provider.StreamEvent{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: active.ID, Name: active.Name}}
					emittedStarts[tc.Index] = true
				}
				if tc.Function != nil && tc.Function.Arguments != "" {
					ch <- provider.StreamEvent{
						Kind:       provider.KindToolUseDelta,
						ToolUseID:  active.ID,
						InputDelta: tc.Function.Arguments,
					}
				}
			}
			if c.FinishReason != nil {
				// Stop any open tool calls.
				for idx := range emittedStarts {
					if emittedStarts[idx] {
						ch <- provider.StreamEvent{Kind: provider.KindToolUseStop, ToolUseID: activeToolCalls[idx].ID}
					}
				}
				ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: *c.FinishReason}
			}
		}
		if chunk.Usage != nil {
			// Pick whichever cache field the upstream surfaced (max
			// because we never want to undercount). Quality is
			// "explicit" if any cache field was non-zero, else "none".
			cacheRead := chunk.Usage.PromptCacheHit
			if chunk.Usage.CacheReadInputTokens > cacheRead {
				cacheRead = chunk.Usage.CacheReadInputTokens
			}
			if chunk.Usage.PromptTokensDetails != nil &&
				chunk.Usage.PromptTokensDetails.CachedTokens > cacheRead {
				cacheRead = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			usage := &provider.Usage{
				InputTokens:      chunk.Usage.PromptTokens,
				OutputTokens:     chunk.Usage.CompletionTokens,
				CacheReadTokens:  cacheRead,
				CacheWriteTokens: chunk.Usage.CacheCreationInputTokens,
			}
			ch <- provider.StreamEvent{Kind: provider.KindUsage, Usage: usage}
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- provider.StreamEvent{Kind: provider.KindError, Err: err}
	}
}

// ErrNoBody is returned when an HTTP response had no body to parse.
var ErrNoBody = errors.New("openai: empty response body")
