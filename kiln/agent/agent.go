package agent

import (
	"context"

	"github.com/gofastr/gofastr/kiln/protocol"
)

// Provider is the LLM transport. Implementations talk to Claude,
// OpenAI, a local model, or anything else. The Loop drives the
// conversation by calling Stream until it sees a turn with no tool
// calls (the "stop" turn).
type Provider interface {
	// Stream runs one provider turn and returns a Turn capturing the
	// assistant's text and any tool calls. Implementations MAY emit
	// streaming events on req.OnEvent (optional).
	Stream(ctx context.Context, req Request) (Turn, error)
}

// Request is one turn's input to the provider.
type Request struct {
	System   string        // composed system prompt (persona + framework + project)
	Messages []Message     // conversation so far (user, assistant, tool_result)
	Tools    []protocol.Descriptor
	OnEvent  func(StreamEvent) // optional streaming callback
}

// Message is one entry in the rolling conversation passed to the provider.
type Message struct {
	Role      string         // "user" | "assistant" | "tool_result"
	Text      string         // for user/assistant text turns
	ToolCalls []ToolCall     // assistant turns may include calls
	ToolUseID string         // for tool_result, references the assistant's call_id
	Result    *protocol.Result // for tool_result, the typed result
}

// ToolCall is the assistant's request to invoke a Kiln tool.
type ToolCall struct {
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

// Turn is the provider's response to one Stream call.
type Turn struct {
	Text      string
	ToolCalls []ToolCall
	StopReason string // "end_turn" | "tool_use" | "max_tokens" | provider-specific
}

// StreamEvent is an optional incremental notification while a turn is
// being produced — text chunk, tool-call start, etc.
type StreamEvent struct {
	Kind string // "text" | "tool_call_start" | "tool_call_done" | "stop"
	Text string
}
