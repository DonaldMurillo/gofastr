package agent_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/agent"
)

// world_get takes no required args. ToolCall.Args is a map, so the
// marshaled buffer dispatch decodes is `null` (nil map), `{}` (empty map)
// or an object with keys; the first two must read the world. An unknown
// key is a validation refusal like any other tool's.
func TestDispatchWorldGetArgs(t *testing.T) {
	ctx := context.Background()
	tools, _ := setupAgent(t)
	for name, args := range map[string]map[string]any{"nil": nil, "empty": {}} {
		if res := agent.Dispatch(ctx, tools, agent.ToolCall{Name: "world_get", Args: args}); !res.OK {
			t.Errorf("world_get with %s args: %+v", name, res)
		}
	}
	res := agent.Dispatch(ctx, tools, agent.ToolCall{Name: "world_get", Args: map[string]any{"no_such_arg": 1}})
	if res.OK || res.Kind != "validation" {
		t.Errorf("world_get with an unknown arg: %+v, want a validation refusal", res)
	}
}
