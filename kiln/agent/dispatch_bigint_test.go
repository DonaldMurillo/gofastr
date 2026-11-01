package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/agent"
)

// A tool call through Dispatch (the agent loop, MCP and ACP adapters)
// keeps an integer past 2^53 with its digits, like the HTTP twin in
// kiln/chat. Dispatch re-encodes the args and decoded them without
// UseNumber, so an `any` slot rounded 9007199254740993 to
// 9007199254740992 before the edit was journaled.
func TestDispatchKeepsBigIntegers(t *testing.T) {
	const exact, rounded = "9007199254740993", "9007199254740992"
	tools, l := setupAgent(t)
	res := agent.Dispatch(context.Background(), tools, agent.ToolCall{
		Name: "add_entity",
		Args: map[string]any{"entity": map[string]any{
			"name":       "items",
			"fields":     []any{map[string]any{"name": "n", "type": "int"}},
			"properties": map[string]any{"big": json.Number(exact)},
		}},
	})
	if !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}
	e := l.Session().World.Entities["items"]
	if e == nil {
		t.Fatal("items entity missing")
	}
	b, err := json.Marshal(e.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Contains(got, rounded) || !strings.Contains(got, exact) {
		t.Errorf("properties = %s, want big = %s", got, exact)
	}
}
