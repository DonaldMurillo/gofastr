package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// fakeProvider emits a scripted sequence of StreamEvents per Chat call.
// Each successive Chat call consumes one element from Scripts.
type fakeProvider struct {
	scripts [][]provider.StreamEvent
	calls   int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.scripts) {
		ch := make(chan provider.StreamEvent)
		close(ch)
		return ch, nil
	}
	ch := make(chan provider.StreamEvent, len(f.scripts[idx]))
	for _, ev := range f.scripts[idx] {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (f *fakeProvider) Models(ctx context.Context) ([]provider.Model, error) { return nil, nil }

func (f *fakeProvider) TokenCount(ctx context.Context, model string, msgs []provider.Message) (int, error) {
	return 0, nil
}

func TestLoopTextOnlyTurn(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	d := NewDispatcher(bus, reg)

	prov := &fakeProvider{
		scripts: [][]provider.StreamEvent{{
			{Kind: provider.KindTextDelta, Text: "Hello there."},
			{Kind: provider.KindStop, FinishReason: "stop"},
		}},
	}
	e := NewEngine(session, bus, prov, "fake-model", d)

	if err := e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("hi")); err != nil {
		t.Fatal(err)
	}
	// History should have user + assistant.
	if len(e.History) != 2 {
		t.Fatalf("history len = %d, want 2", len(e.History))
	}
	if e.History[1].Role != provider.RoleAssistant {
		t.Errorf("assistant role wrong: %v", e.History[1])
	}
}

func TestLoopToolUseCycle(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), staticSource{
		name: "test", tools: []tool.Tool{fakeTool{name: "Echo", mutating: false, out: "echo-result"}},
	}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(bus, reg)

	// First Chat: model emits a tool_use.
	// Second Chat: model emits text after seeing the tool_result.
	toolUseID := "call_provider_id"
	prov := &fakeProvider{
		scripts: [][]provider.StreamEvent{
			{
				{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: toolUseID, Name: "Echo"}},
				{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
				{Kind: provider.KindToolUseStop},
				{Kind: provider.KindStop, FinishReason: "tool_use"},
			},
			{
				{Kind: provider.KindTextDelta, Text: "Tool returned: echo-result"},
				{Kind: provider.KindStop, FinishReason: "stop"},
			},
		},
	}
	e := NewEngine(session, bus, prov, "fake-model", d)

	if err := e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("call Echo")); err != nil {
		t.Fatal(err)
	}
	// History should be: user, assistant(tool_use), user(tool_result), assistant(text).
	if len(e.History) != 4 {
		t.Fatalf("history len = %d, want 4: %s", len(e.History), FormatMessages(e.History))
	}
	if prov.calls != 2 {
		t.Errorf("provider Chat calls = %d, want 2", prov.calls)
	}
	// Tool result message should reference the original tool_use_id.
	toolResultMsg := e.History[2]
	if len(toolResultMsg.Content) != 1 || toolResultMsg.Content[0].ToolResult == nil ||
		toolResultMsg.Content[0].ToolResult.ToolUseID != toolUseID {
		blob, _ := json.Marshal(toolResultMsg)
		t.Errorf("tool result message wrong: %s", blob)
	}
}

func TestLoopEmitsTurnTiming(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	d := NewDispatcher(bus, reg)
	prov := &fakeProvider{scripts: [][]provider.StreamEvent{{
		{Kind: provider.KindTextDelta, Text: "ok"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}}
	e := NewEngine(session, bus, prov, "m", d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.Subscribe(ctx)

	_ = e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("hi"))
	kinds := drain(sub, 200*time.Millisecond)

	var gotTiming bool
	for _, k := range kinds {
		if k == "TurnTiming" {
			gotTiming = true
			break
		}
	}
	if !gotTiming {
		t.Errorf("missing TurnTiming event in %v", kinds)
	}
}
