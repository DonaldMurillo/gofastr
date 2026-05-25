package builtins

// Agent — the harness's sub-agent launcher. Pattern from Claude Code's
// "Task" tool: the model can spawn a fresh agent with a focused prompt
// and get back a final summary. The sub-agent has the same Provider,
// Model, and Tools as the parent but a blank conversation history, so
// long parent contexts don't bleed into the sub-task.
//
// Implementation: the engine implements tool.Spawner and is plumbed
// into the dispatch ctx by Engine.RunTurn. The tool resolves it and
// calls Spawn(systemHint, userPrompt), blocking until the sub-agent's
// loop terminates. Events from the sub-agent fan out on the same bus,
// so attached clients can watch its progress in real time.
//
// Defaults:
//
//   - systemHint is empty unless the caller passes one. The sub-agent
//     inherits the parent's system prompt + AGENTS.md via the engine's
//     request middleware regardless.
//   - The result is the sub-agent's last assistant text. Tool-call
//     interactions inside the sub-loop are hidden from the parent
//     (just like CC's Task tool returns a single summary).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

type Agent struct{}

func (Agent) Name() string { return "Agent" }
func (Agent) Description() string {
	return "Spawn a sub-agent with a focused prompt and return its final answer. " +
		"Use for tasks that don't need the full conversation context, or to do parallelizable focused work. " +
		"The sub-agent has the same tools but starts with an empty history. " +
		"Inputs: prompt (required, what the sub-agent should do), description (short label for UI), system (optional extra system hint)."
}
func (Agent) Mutating() bool { return true } // sub-agents may dispatch mutating tools
func (Agent) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "prompt":      {"type": "string", "description": "The task the sub-agent should perform — phrased as a direct request."},
    "description": {"type": "string", "description": "Short 3-5 word label shown in clients while the sub-agent runs."},
    "system":      {"type": "string", "description": "Optional extra system hint prepended to the sub-agent's history."}
  },
  "required": ["prompt"],
  "additionalProperties": false
}`)
}

type agentArgs struct {
	Prompt      string `json:"prompt"`
	Description string `json:"description,omitempty"`
	System      string `json:"system,omitempty"`
}

func (Agent) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args agentArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Agent: invalid arguments: %w", err)
	}
	if args.Prompt == "" {
		return errorResult("Agent: prompt is required"), nil
	}
	sp, ok := tool.SpawnerFromContext(ctx)
	if !ok {
		return errorResult("Agent: no spawner in dispatch context (engine wiring missing)"), nil
	}
	// Depth guard — refuse if this Agent call is already inside a
	// sub-agent at the max depth. Prevents recursive Agent → Agent →
	// Agent chains that ran a single session up to 70+ tool calls
	// (sess_01KSDZK5…).
	depth := tool.SpawnDepthFromContext(ctx)
	if depth >= tool.MaxSpawnDepth {
		return errorResult(fmt.Sprintf("Agent: refusing to spawn at depth %d (MaxSpawnDepth=%d). "+
			"You are already inside a sub-agent — finish the work directly here instead of recursing further.",
			depth, tool.MaxSpawnDepth)), nil
	}
	answer, err := sp.Spawn(ctx, args.System, args.Prompt)
	if err != nil {
		return errorResult(fmt.Sprintf("Agent: sub-agent failed: %v", err)), nil
	}
	if answer == "" {
		answer = "(sub-agent produced no final assistant text)"
	}
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: answer}},
	}, nil
}
