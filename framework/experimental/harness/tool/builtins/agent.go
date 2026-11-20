package builtins

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Agent runs a sub-agent with the given prompt and returns its final assistant text.
type Agent struct{}

func (Agent) Name() string { return "Agent" }
func (Agent) Description() string {
	return "Spawn a sub-agent with a focused prompt and return its final answer. " +
		"Use for tasks that don't need the full conversation context, or to do parallelizable focused work. " +
		"The sub-agent has the same tools but starts with an empty history. " +
		"Inputs: prompt (required, what the sub-agent should do), description (short label for UI), system (optional extra system hint)."
}
func (Agent) Mutating() bool { return true }
func (Agent) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "prompt":      {"type": "string", "description": "The task the sub-agent should perform, phrased as a direct request."},
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
	depth := tool.SpawnDepthFromContext(ctx)
	if depth >= tool.MaxSpawnDepth {
		return errorResult(fmt.Sprintf("Agent: refusing to spawn at depth %d (MaxSpawnDepth=%d). "+
			"You are already inside a sub-agent: finish the work directly here instead of recursing further.",
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
