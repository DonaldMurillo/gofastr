package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gofastr/gofastr/kiln/protocol"
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
	// point for retrieval-augmented context — see [NewEmbedContextHook]
	// for the wiring against a battery/embed index.
	ContextHook func(ctx context.Context, userText string) string

	messages []Message
}

// Run feeds userText to the loop and drives provider turns until the
// provider stops or MaxTurns is reached.
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

	for turn := 0; turn < l.MaxTurns; turn++ {
		system := BuildPrompt(l.Tools.Live().Session(), l.Tools.List()).String()
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
		for _, call := range t.ToolCalls {
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

// dispatch is shared by the Loop and the MCP/ACP adapters. Single switch.
func dispatch(ctx context.Context, t *protocol.Tools, call ToolCall) protocol.Result {
	buf, err := json.Marshal(call.Args)
	if err != nil {
		return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
	}
	dec := func(out any) error { return json.Unmarshal(buf, out) }

	switch call.Name {
	case "world_get":
		var a protocol.WorldGetArgs
		if err := dec(&a); err != nil && len(buf) > 2 {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.WorldGet(ctx, a)
	case "set_app_config":
		var a protocol.SetAppConfigArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.SetAppConfig(ctx, a)
	case "add_entity":
		var a protocol.AddEntityArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddEntity(ctx, a)
	case "update_entity":
		var a protocol.UpdateEntityArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.UpdateEntity(ctx, a)
	case "delete_entity":
		var a protocol.DeleteEntityArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.DeleteEntity(ctx, a)
	case "add_field":
		var a protocol.AddFieldArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddField(ctx, a)
	case "delete_field":
		var a protocol.DeleteFieldArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.DeleteField(ctx, a)
	case "add_page":
		var a protocol.AddPageArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddPage(ctx, a)
	case "delete_page":
		var a protocol.DeletePageArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.DeletePage(ctx, a)
	case "update_page_element":
		var a protocol.UpdatePageElementArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.UpdatePageElement(ctx, a)
	case "add_hook":
		var a protocol.AddHookArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddHook(ctx, a)
	case "delete_hook":
		var a protocol.DeleteHookArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.DeleteHook(ctx, a)
	case "add_route":
		var a protocol.AddRouteArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddRoute(ctx, a)
	case "delete_route":
		var a protocol.DeleteRouteArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.DeleteRoute(ctx, a)
	case "add_seed":
		var a protocol.AddSeedArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.AddSeed(ctx, a)
	case "propose_plan":
		var a protocol.ProposePlanArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.ProposePlan(ctx, a)
	case "approve_plan":
		var a protocol.ApprovePlanArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.ApprovePlan(ctx, a)
	case "undo":
		return t.Undo(ctx, protocol.UndoArgs{})
	case "chat":
		var a protocol.ChatArgs
		if err := dec(&a); err != nil {
			return protocol.Result{OK: false, Error: err.Error(), Kind: "validation"}
		}
		return t.Chat(ctx, a)
	}
	return protocol.Result{OK: false, Error: "unknown tool: " + call.Name, Kind: "not_found", Hint: "call only the tool names listed in the framework slab"}
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
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Text
		}
	}
	return ""
}
