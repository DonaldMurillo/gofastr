package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/DonaldMurillo/gofastr/kiln/internal/toolargs"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// Loop runs a multi-turn tool-use conversation against a Provider.
// Each user message produces zero or more provider turns; each turn
// may emit text and/or tool calls. Tool calls are dispatched through
// the Tools surface (which journals them via Live), and their results
// are appended to the conversation for the next turn.
type Loop struct {
	Provider Provider
	Tools    *protocol.Tools

	// MaxTurns caps a single Run; 0 → 16. Prevents runaway loops.
	MaxTurns int

	// OnEvent receives StreamEvents from the provider plus loop-level
	// events (tool_call_dispatched, tool_call_returned). Optional.
	OnEvent func(StreamEvent)

	// ContextHook is called once per turn with the most recent user
	// message text. Its return value is prepended to the provider's
	// System prompt (separated by a blank line). It is the integration
	// point for retrieval-augmented context. See [NewSemanticContextHook]
	// for the wiring against a battery/semantic index.
	ContextHook func(ctx context.Context, userText string) string

	messages []Message
}

// Run feeds userText to the loop and drives provider turns until the
// provider stops or MaxTurns is reached. A cancelled ctx stops it with
// ctx's error and nothing changes behind the cancel: a turn the provider
// returns after it journals no text and runs no tool calls, and the calls
// left in a turn when ctx ends between dispatches are answered "not run"
// so the next Run never sends a tool call without a result. The protocol
// mutations ignore ctx, so these checks are the only stop. kiln/acp's
// runTurns keeps the same guards.
func (l *Loop) Run(ctx context.Context, userText string) error {
	if l.Provider == nil {
		return fmt.Errorf("agent: nil Provider")
	}
	if l.Tools == nil {
		return fmt.Errorf("agent: nil Tools")
	}
	if l.MaxTurns <= 0 {
		l.MaxTurns = 16
	}
	// Journal the user message via Tools so it shows up in the panel.
	l.Tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: userText})
	l.messages = append(l.messages, Message{Role: "user", Text: userText})
	for turn := range l.MaxTurns {
		if err := ctx.Err(); err != nil {
			return err
		}
		system := BuildPromptLive(l.Tools.Live(), l.Tools.List()).String()
		if l.ContextHook != nil {
			if extra := l.ContextHook(ctx, lastUserText(l.messages)); extra != "" {
				system = extra + "\n\n" + system
			}
		}
		req := Request{
			System:   system,
			Messages: l.messages,
			Tools:    l.Tools.List(),
			OnEvent:  l.OnEvent,
		}
		t, err := l.Provider.Stream(ctx, req)
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return fmt.Errorf("provider turn %d: %w", turn, err)
		}
		// Journal the assistant's text (if any).
		if t.Text != "" {
			l.Tools.Chat(ctx, protocol.ChatArgs{Role: "assistant", Text: t.Text})
		}
		assistantMsg := Message{Role: "assistant", Text: t.Text, ToolCalls: t.ToolCalls}
		l.messages = append(l.messages, assistantMsg)

		if len(t.ToolCalls) == 0 {
			return nil
		}
		// Dispatch each tool call and append its result.
		for i, call := range t.ToolCalls {
			if err := ctx.Err(); err != nil {
				l.messages = append(l.messages, notRun(t.ToolCalls[i:])...)
				return err
			}
			res := l.dispatchTool(ctx, call)
			l.messages = append(l.messages, Message{
				Role:      "tool_result",
				ToolUseID: call.CallID,
				Result:    &res,
			})
		}
	}
	return fmt.Errorf("agent: exceeded MaxTurns (%d)", l.MaxTurns)
}

// notRun answers the tool calls a cancelled run never dispatched.
func notRun(calls []ToolCall) []Message {
	out := make([]Message, 0, len(calls))
	for _, call := range calls {
		res := protocol.Result{OK: false, Kind: "cancelled", Error: "not run: the run was cancelled"}
		out = append(out, Message{Role: "tool_result", ToolUseID: call.CallID, Result: &res})
	}
	return out
}

// dispatchTool routes a tool call by name through the Tools surface.
// Errors at the dispatch layer (unknown tool, bad args) come back as
// not-OK results so the agent can self-correct.
func (l *Loop) dispatchTool(ctx context.Context, call ToolCall) protocol.Result {
	if l.OnEvent != nil {
		l.OnEvent(StreamEvent{Kind: "tool_call_dispatched", Text: call.Name})
	}
	res := dispatch(ctx, l.Tools, call)
	if l.OnEvent != nil {
		l.OnEvent(StreamEvent{Kind: "tool_call_returned", Text: call.Name})
	}
	return res
}

// dispatch is shared by the Loop and the MCP/ACP adapters. The tool table
// is toolargs.Call, the same one the chat HTTP endpoint uses.
func dispatch(ctx context.Context, t *protocol.Tools, call ToolCall) protocol.Result {
	buf, err := json.Marshal(call.Args)
	if err != nil {
		return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
	}
	// The model is an untrusted-ish peer for decode purposes: toolargs
	// decodes strictly (case-folded duplicate keys are refused, never
	// folded last-wins onto the typed args) and exactly (an integer past
	// 2^53 keeps its digits).
	res, err := toolargs.Call(ctx, t, call.Name, buf)
	if errors.Is(err, toolargs.ErrUnknownTool) {
		return protocol.Result{OK: false, Error: "unknown tool: " + call.Name, Kind: "not_found", Hint: "call only the tool names listed in the framework slab"}
	}
	if err != nil {
		return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
	}
	return res
}

// Dispatch is exposed for the MCP/ACP transports to share the same
// switch as the native Loop.
func Dispatch(ctx context.Context, t *protocol.Tools, call ToolCall) protocol.Result {
	return dispatch(ctx, t, call)
}

// lastUserText returns the text of the most recent "user" message, or
// "" if there is none. Used by ContextHook to score retrieval against
// the live user intent rather than the full transcript.
func lastUserText(msgs []Message) string {
	for _, msg := range slices.Backward(msgs) {
		if msg.Role == "user" {
			return msg.Text
		}
	}
	return ""
}
