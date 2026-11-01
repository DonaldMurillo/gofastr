package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/agent"
)

// cancelProvider cancels the run's context while its turn is in flight,
// the way a provider that finishes its request after a cancel does.
type cancelProvider struct {
	cancel context.CancelFunc
	turn   agent.Turn
	saw    []agent.Request
}

func (p *cancelProvider) Stream(_ context.Context, req agent.Request) (agent.Turn, error) {
	p.saw = append(p.saw, req)
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		return p.turn, nil
	}
	return agent.Turn{StopReason: "end_turn"}, nil
}

func addEntityCall(id, name string) agent.ToolCall {
	return agent.ToolCall{CallID: id, Name: "add_entity", Args: map[string]any{
		"entity": map[string]any{"name": name, "fields": []any{map[string]any{"name": "title", "type": "string"}}},
	}}
}

// A cancelled run changes nothing behind the cancel: the turn that came
// back after it neither journals its text nor runs its tool calls.
func TestLoopCancelledTurnRunsNoTools(t *testing.T) {
	tools, _ := setupAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &cancelProvider{cancel: cancel, turn: agent.Turn{
		Text:      "adding",
		ToolCalls: []agent.ToolCall{addEntityCall("c1", "posts"), addEntityCall("c2", "tags")},
	}}
	loop := &agent.Loop{Provider: prov, Tools: tools}
	if err := loop.Run(ctx, "go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if n := len(tools.Live().Session().World.Entities); n != 0 {
		t.Fatalf("cancelled run added %d entities", n)
	}
	for _, c := range tools.Live().Session().Chat {
		if c.Message != nil && c.Message.Text == "adding" {
			t.Fatal("cancelled run journaled the late turn's text")
		}
	}
}

// A cancel between dispatches stops the rest of the turn's calls, and each
// call that did not run still gets a result, so the next Run never sends
// a tool call without one.
func TestLoopCancelStopsRemainingCalls(t *testing.T) {
	tools, _ := setupAgent(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &fakeProvider{turns: []agent.Turn{{
		ToolCalls: []agent.ToolCall{addEntityCall("c1", "posts"), addEntityCall("c2", "tags")},
	}}}
	loop := &agent.Loop{Provider: prov, Tools: tools, OnEvent: func(ev agent.StreamEvent) {
		if ev.Kind == "tool_call_returned" {
			cancel()
		}
	}}
	if err := loop.Run(ctx, "go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	ents := tools.Live().Session().World.Entities
	if _, ok := ents["posts"]; !ok || len(ents) != 1 {
		t.Fatalf("entities = %v, want only posts (c2 ran after the cancel)", ents)
	}
	loop.OnEvent = nil
	if err := loop.Run(context.Background(), "again"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	last := prov.saw[len(prov.saw)-1].Messages
	results := map[string]string{}
	for _, m := range last {
		if m.Role == "tool_result" && m.Result != nil {
			results[m.ToolUseID] = m.Result.Kind
		}
	}
	if _, ok := results["c1"]; !ok {
		t.Errorf("c1 has no tool_result: %v", results)
	}
	if results["c2"] != "cancelled" {
		t.Errorf("c2 result kind = %q, want cancelled", results["c2"])
	}
}
