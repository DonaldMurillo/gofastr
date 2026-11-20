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

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// Client speaks the OpenAI Chat Completions wire format with SSE streaming.
// A nil HTTP uses a client that refuses redirects; Name prefixes its errors.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Headers map[string]string
	Name    string
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
		client = &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("%s: HTTP %d: %s", c.Name, resp.StatusCode, textsafe.StripUnsafe(string(raw)))
	}

	ch := make(chan provider.StreamEvent, 32)
	go parseSSEStream(ctx, resp.Body, ch)
	return ch, nil
}

type requestBody struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function openaiToolCallFn `json:"function"`
}

type openaiToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiTool struct {
	Type     string             `json:"type"`
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

func translateOutbound(m provider.Message) []openaiMessage {
	switch m.Role {
	case provider.RoleAssistant:
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
		var contentField any
		if t := text.String(); t != "" {
			contentField = t
		} else if len(toolCalls) == 0 {
			contentField = ""
		}
		return []openaiMessage{{
			Role:      "assistant",
			Content:   contentField,
			ToolCalls: toolCalls,
		}}
	case provider.RoleUser:
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

type streamChunk struct {
	Choices []streamChoice `json:"choices"`
	Usage   *struct {
		PromptTokens             int `json:"prompt_tokens"`
		CompletionTokens         int `json:"completion_tokens"`
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
		Content          string          `json:"content,omitempty"`
		ToolCalls        []deltaToolCall `json:"tool_calls,omitempty"`
		ReasoningContent string          `json:"reasoning_content,omitempty"`
		Reasoning        string          `json:"reasoning,omitempty"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type deltaToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Function *deltaToolCallFunc `json:"function,omitempty"`
}

type deltaToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func parseSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamEvent) {
	defer body.Close()
	defer close(ch)
	send := func(ev provider.StreamEvent) bool {
		select {
		case ch <- ev:
			return true
		default:
		}
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	activeToolCalls := map[int]*control.ToolUse{}
	emittedStarts := map[int]bool{}
	sawTerminal := false

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			if !send(provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}) {
				return
			}
			return
		}
		var chunk streamChunk
		if err := handler.CheckObjectKeys([]byte(payload), handler.FoldJSONKey); err != nil {
			if !send(provider.StreamEvent{Kind: provider.KindError, Err: fmt.Errorf("parse chunk: %w", err)}) {
				return
			}
			return
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			if !send(provider.StreamEvent{Kind: provider.KindError, Err: fmt.Errorf("parse chunk: %w", err)}) {
				return
			}
			return
		}
		for _, c := range chunk.Choices {
			if reasoning := c.Delta.ReasoningContent + c.Delta.Reasoning; reasoning != "" {
				if quoted, err := json.Marshal(reasoning); err == nil {
					if !send(provider.StreamEvent{Kind: provider.KindThinkingDelta, Thinking: quoted}) {
						return
					}
				}
			}
			if c.Delta.Content != "" {
				if !send(provider.StreamEvent{Kind: provider.KindTextDelta, Text: c.Delta.Content}) {
					return
				}
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
				if !emittedStarts[tc.Index] && active.Name != "" {
					if !send(provider.StreamEvent{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: active.ID, Name: active.Name}}) {
						return
					}
					emittedStarts[tc.Index] = true
				}
				if tc.Function != nil && tc.Function.Arguments != "" {
					if !send(provider.StreamEvent{
						Kind:       provider.KindToolUseDelta,
						ToolUseID:  active.ID,
						InputDelta: tc.Function.Arguments,
					}) {
						return
					}
				}
			}
			if c.FinishReason != nil {
				for idx := range emittedStarts {
					if emittedStarts[idx] {
						if !send(provider.StreamEvent{Kind: provider.KindToolUseStop, ToolUseID: activeToolCalls[idx].ID}) {
							return
						}
					}
				}
				if !send(provider.StreamEvent{Kind: provider.KindStop, FinishReason: *c.FinishReason}) {
					return
				}
				sawTerminal = true
			}
		}
		if chunk.Usage != nil {
			cacheRead := max(chunk.Usage.CacheReadInputTokens, chunk.Usage.PromptCacheHit)
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
			if !send(provider.StreamEvent{Kind: provider.KindUsage, Usage: usage}) {
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if !send(provider.StreamEvent{Kind: provider.KindError, Err: err}) {
			return
		}
		return
	}
	if !sawTerminal {
		if !send(provider.StreamEvent{Kind: provider.KindError, Err: ErrStreamTruncated}) {
			return
		}
	}
}

// ErrStreamTruncated is the error event sent when the SSE body ends without [DONE] or a finish_reason.
var ErrStreamTruncated = errors.New("openai: stream ended without a terminal event")

// ErrNoBody is returned when an HTTP response had no body to parse.
var ErrNoBody = errors.New("openai: empty response body")
