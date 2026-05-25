package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// fakeSpawner implements tool.Spawner for unit-testing the Agent tool
// without spinning a real engine.
type fakeSpawner struct {
	wantSystem string
	wantPrompt string
	answer     string
	err        error
}

func (f *fakeSpawner) Spawn(_ context.Context, system, prompt string) (string, error) {
	f.wantSystem = system
	f.wantPrompt = prompt
	return f.answer, f.err
}

func TestAgentForwardsToSpawner(t *testing.T) {
	sp := &fakeSpawner{answer: "found it on line 42"}
	ctx := tool.WithSpawner(context.Background(), sp)
	raw, _ := json.Marshal(map[string]any{
		"prompt":      "find the bug in main.go",
		"description": "find bug",
		"system":      "you are a debugger",
	})
	res, err := Agent{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Errorf("unexpected error result: %+v", res)
	}
	if sp.wantPrompt != "find the bug in main.go" {
		t.Errorf("prompt = %q, want exact", sp.wantPrompt)
	}
	if sp.wantSystem != "you are a debugger" {
		t.Errorf("system = %q, want exact", sp.wantSystem)
	}
	if res.Content[0].Text != "found it on line 42" {
		t.Errorf("body = %q, want passthrough", res.Content[0].Text)
	}
}

func TestAgentRequiresPrompt(t *testing.T) {
	ctx := tool.WithSpawner(context.Background(), &fakeSpawner{})
	raw, _ := json.Marshal(map[string]any{"description": "no prompt"})
	res, _ := Agent{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if !res.IsError {
		t.Errorf("missing prompt should be an error result, got %+v", res)
	}
}

func TestAgentNoSpawnerInCtxIsErrorNotPanic(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"prompt": "do x"})
	res, err := Agent{}.Run(context.Background(), tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no spawner") {
		t.Errorf("expected no-spawner error, got %+v", res)
	}
}

// TestAgentRefusesPastMaxDepth: an Agent call at depth >= MaxSpawnDepth
// must NOT trigger another Spawn — it must return an error result so
// the model knows recursion is bounded. Regression for the runaway
// session bug (sess_01KSDZK5… — 10 Agent spawns chained).
func TestAgentRefusesPastMaxDepth(t *testing.T) {
	sp := &fakeSpawner{answer: "should never run"}
	// Simulate being CALLED from inside a sub-agent at the max depth.
	ctx := tool.WithSpawner(context.Background(), sp)
	ctx = tool.WithSpawnDepth(ctx, tool.MaxSpawnDepth)
	raw, _ := json.Marshal(map[string]any{"prompt": "do x", "description": "x"})
	res, err := Agent{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("Agent at max depth should return an error result, got: %+v", res)
	}
	if sp.wantPrompt != "" {
		t.Errorf("Agent at max depth must NOT call Spawner — but it did (prompt=%q)", sp.wantPrompt)
	}
	if !strings.Contains(res.Content[0].Text, "depth") {
		t.Errorf("error text should mention depth: %q", res.Content[0].Text)
	}
}

// TestAgentAllowedUnderMaxDepth: at depth < max, Agent still spawns.
// Same fakeSpawner setup, but depth is below the cap.
func TestAgentAllowedUnderMaxDepth(t *testing.T) {
	sp := &fakeSpawner{answer: "ok"}
	ctx := tool.WithSpawner(context.Background(), sp)
	ctx = tool.WithSpawnDepth(ctx, tool.MaxSpawnDepth-1)
	raw, _ := json.Marshal(map[string]any{"prompt": "ok"})
	res, _ := Agent{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if res.IsError {
		t.Errorf("Agent at depth < max should succeed, got: %+v", res)
	}
	if sp.wantPrompt != "ok" {
		t.Errorf("Agent did not invoke spawner: %+v", sp)
	}
}

func TestAgentPropagatesSpawnerError(t *testing.T) {
	sp := &fakeSpawner{err: errors.New("provider HTTP 500")}
	ctx := tool.WithSpawner(context.Background(), sp)
	raw, _ := json.Marshal(map[string]any{"prompt": "do x"})
	res, _ := Agent{}.Run(ctx, tool.ToolCall{Input: raw}, nil)
	if !res.IsError {
		t.Errorf("expected error result on spawn failure, got %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "provider HTTP 500") {
		t.Errorf("error text missing upstream message: %q", res.Content[0].Text)
	}
}
