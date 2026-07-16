package agent_test

import (
	"context"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// fakeProvider replays a scripted sequence of turns. Each Stream call
// returns the next turn from the queue.
type fakeProvider struct {
	turns []agent.Turn
	idx   int
	saw   []agent.Request
}

func (f *fakeProvider) Stream(_ context.Context, req agent.Request) (agent.Turn, error) {
	f.saw = append(f.saw, req)
	if f.idx >= len(f.turns) {
		return agent.Turn{StopReason: "end_turn"}, nil
	}
	t := f.turns[f.idx]
	f.idx++
	return t, nil
}

func setupAgent(t *testing.T) (*protocol.Tools, *live.Live) {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l), l
}

func TestLoopSingleTextTurn(t *testing.T) {
	tools, _ := setupAgent(t)
	prov := &fakeProvider{turns: []agent.Turn{
		{Text: "hello back", StopReason: "end_turn"},
	}}
	loop := &agent.Loop{Provider: prov, Tools: tools}
	if err := loop.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	chat := tools.Live().Session().Chat
	if len(chat) != 2 {
		t.Fatalf("expected 2 chat events (user + assistant), got %d", len(chat))
	}
	if chat[0].Message == nil || chat[0].Message.Text != "hello" {
		t.Errorf("user message wrong: %+v", chat[0])
	}
	if chat[1].Message == nil || chat[1].Message.Text != "hello back" {
		t.Errorf("assistant message wrong: %+v", chat[1])
	}
}

func TestLoopToolCallExecutes(t *testing.T) {
	tools, _ := setupAgent(t)
	prov := &fakeProvider{turns: []agent.Turn{
		{
			ToolCalls: []agent.ToolCall{{
				CallID: "c1",
				Name:   "add_entity",
				Args: map[string]any{
					"entity": map[string]any{
						"name": "posts",
						"fields": []any{
							map[string]any{"name": "title", "type": "string"},
						},
					},
				},
			}},
		},
		{Text: "added posts", StopReason: "end_turn"},
	}}
	loop := &agent.Loop{Provider: prov, Tools: tools}
	if err := loop.Run(context.Background(), "make a posts entity"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := tools.Live().Session().World.Entities["posts"]; !ok {
		t.Error("posts not added by the tool call")
	}
	// Provider should have been called twice.
	if got := len(prov.saw); got != 2 {
		t.Errorf("provider called %d times, want 2", got)
	}
	// Second call should include the tool_result message.
	last := prov.saw[1]
	if len(last.Messages) < 3 {
		t.Fatalf("second turn messages = %d, want >= 3", len(last.Messages))
	}
	if last.Messages[2].Role != "tool_result" {
		t.Errorf("third message role = %q, want tool_result", last.Messages[2].Role)
	}
}

func TestLoopToolErrorPropagatedAsResult(t *testing.T) {
	tools, _ := setupAgent(t)
	prov := &fakeProvider{turns: []agent.Turn{
		{ToolCalls: []agent.ToolCall{{CallID: "c1", Name: "add_field", Args: map[string]any{
			"entity": "missing", "field": map[string]any{"name": "x", "type": "string"},
		}}}},
		{Text: "ok", StopReason: "end_turn"},
	}}
	loop := &agent.Loop{Provider: prov, Tools: tools}
	if err := loop.Run(context.Background(), "add field"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	last := prov.saw[1]
	tr := last.Messages[2]
	if tr.Result == nil || tr.Result.OK {
		t.Errorf("expected not-OK tool result, got %+v", tr.Result)
	}
	if tr.Result.Kind != "not_found" {
		t.Errorf("expected kind=not_found, got %q", tr.Result.Kind)
	}
}

func TestLoopMaxTurnsCap(t *testing.T) {
	tools, _ := setupAgent(t)
	// Provider keeps emitting tool calls forever — loop should cap.
	prov := &fakeProvider{turns: nil}
	for i := 0; i < 10; i++ {
		prov.turns = append(prov.turns, agent.Turn{ToolCalls: []agent.ToolCall{{CallID: "c", Name: "world_get", Args: map[string]any{}}}})
	}
	loop := &agent.Loop{Provider: prov, Tools: tools, MaxTurns: 3}
	err := loop.Run(context.Background(), "go")
	if err == nil {
		t.Fatal("expected MaxTurns error")
	}
	if !strings.Contains(err.Error(), "MaxTurns") {
		t.Errorf("error = %v", err)
	}
}

func TestPromptLayerStringOrdering(t *testing.T) {
	p := agent.PromptLayers{
		Persona:   "PERSONA_TEXT",
		Framework: "FRAMEWORK_TEXT",
		Project:   "PROJECT_TEXT",
	}
	s := p.String()
	personaIdx := strings.Index(s, "PERSONA_TEXT")
	frameworkIdx := strings.Index(s, "FRAMEWORK_TEXT")
	projectIdx := strings.Index(s, "PROJECT_TEXT")
	if !(personaIdx < frameworkIdx && frameworkIdx < projectIdx) {
		t.Errorf("layers out of order: persona=%d framework=%d project=%d", personaIdx, frameworkIdx, projectIdx)
	}
}

func TestProjectSlabIncludesEntitiesAndPages(t *testing.T) {
	tools, _ := setupAgent(t)
	tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	tools.AddPage(t.Context(), protocol.AddPageArgs{Page: &world.Page{Path: "/", Tree: world.Node{Kind: "div"}}})
	slab := agent.BuildProjectSlab(tools.Live().Session())
	if !strings.Contains(slab, "posts") {
		t.Errorf("slab missing entity: %s", slab)
	}
	if !strings.Contains(slab, "/") {
		t.Errorf("slab missing page: %s", slab)
	}
}

func TestFrameworkSlabListsTools(t *testing.T) {
	tools, _ := setupAgent(t)
	slab := agent.BuildFrameworkSlab(tools.List())
	for _, want := range []string{"add_entity", "delete_entity", "world_get", "undo", "destructive"} {
		if !strings.Contains(slab, want) {
			t.Errorf("framework slab missing %q: %s", want, slab)
		}
	}
}

func TestDispatchExposesShared(t *testing.T) {
	// MCP/ACP adapters should be able to share the same dispatch logic.
	tools, _ := setupAgent(t)
	res := agent.Dispatch(context.Background(), tools, agent.ToolCall{
		Name: "add_entity",
		Args: map[string]any{
			"entity": map[string]any{
				"name":   "x",
				"fields": []any{map[string]any{"name": "y", "type": "string"}},
			},
		},
	})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
}

func TestDispatchCoversNativeMetaAndScaffoldTools(t *testing.T) {
	tools, _ := setupAgent(t)
	for _, tc := range []agent.ToolCall{
		{Name: "set_scaffold", Args: map[string]any{"nav": []any{map[string]any{"label": "Home", "href": "/"}}}},
		{Name: "set_theme", Args: map[string]any{"theme": map[string]any{"primary": "#3366ff"}}},
	} {
		if res := agent.Dispatch(context.Background(), tools, tc); !res.OK {
			t.Errorf("dispatch %s: %+v", tc.Name, res)
		}
	}
}
