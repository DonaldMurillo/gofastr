package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/permission"
)

func echoEngine(t *testing.T, scripts [][]provider.StreamEvent) (*Engine, *fakeProvider) {
	t.Helper()
	session := ids.NewSessionID()
	bus := NewBus(session)
	t.Cleanup(bus.Close)
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), staticSource{
		name: "test", tools: []tool.Tool{fakeTool{name: "Echo", out: "ok"}},
	}); err != nil {
		t.Fatal(err)
	}
	prov := &fakeProvider{scripts: scripts}
	return NewEngine(session, bus, prov, "m", NewDispatcher(bus, reg)), prov
}

func echoCall() []provider.StreamEvent {
	return []provider.StreamEvent{
		{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "c", Name: "Echo"}},
		{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
		{Kind: provider.KindToolUseStop},
		{Kind: provider.KindStop, FinishReason: "tool_use"},
	}
}

func textReply() []provider.StreamEvent {
	return []provider.StreamEvent{
		{Kind: provider.KindTextDelta, Text: "done"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}
}

func events(t *testing.T, b *Bus) []control.Event {
	t.Helper()
	var out []control.Event
	for _, env := range b.Backlog(context.Background(), 0, 0) {
		if env.ID == 0 {
			continue
		}
		ev, err := control.DecodeEvent(env)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func turnEnds(t *testing.T, b *Bus) []control.TurnEnded {
	var out []control.TurnEnded
	for _, ev := range events(t, b) {
		if te, ok := ev.(control.TurnEnded); ok {
			out = append(out, te)
		}
	}
	return out
}

func TestRunTurnStopsAtIterationCap(t *testing.T) {
	scripts := make([][]provider.StreamEvent, 2*maxInnerLoopIterations)
	for i := range scripts {
		scripts[i] = echoCall()
	}
	e, prov := echoEngine(t, scripts)
	if err := e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("go")); err != nil {
		t.Fatal(err)
	}
	if prov.calls != 25 {
		t.Fatalf("provider rounds = %d, want 25", prov.calls)
	}
	if te := turnEnds(t, e.Bus); len(te) != 1 || te[0].Reason != "iteration_limit" {
		t.Fatalf("TurnEnded = %+v", te)
	}
}

func TestRunTurnEmptyResponseIsAnError(t *testing.T) {
	e, _ := echoEngine(t, [][]provider.StreamEvent{{{Kind: provider.KindStop, FinishReason: "stop"}}})
	_ = e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("hi"))
	var sawErr bool
	for _, ev := range events(t, e.Bus) {
		if er, ok := ev.(control.Error); ok && er.Reason == "EmptyResponse" {
			sawErr = true
		}
	}
	if te := turnEnds(t, e.Bus); !sawErr || len(te) != 1 || te[0].Reason != "empty" {
		t.Fatalf("empty reply: Error seen %v, TurnEnded %+v", sawErr, te)
	}
}

func TestTurnNumberSkipsToolResultRounds(t *testing.T) {
	e, _ := echoEngine(t, [][]provider.StreamEvent{echoCall(), echoCall(), textReply(), textReply()})
	for range 2 {
		if err := e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("go")); err != nil {
			t.Fatal(err)
		}
	}
	te := turnEnds(t, e.Bus)
	if len(te) != 2 || te[0].Turn != 1 || te[1].Turn != 2 {
		t.Fatalf("turn numbers = %+v, want 1 then 2", te)
	}
}

func TestOnTurnEndRunsBeforeTurnEnded(t *testing.T) {
	e, _ := echoEngine(t, [][]provider.StreamEvent{textReply()})
	var idAtHook uint64
	e.OnTurnEnd = func() { idAtHook = e.Bus.NextID() }
	_ = e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("hi"))
	for _, env := range e.Bus.Backlog(context.Background(), 0, 0) {
		if env.Kind == "TurnEnded" && env.ID < idAtHook {
			t.Fatalf("OnTurnEnd ran after TurnEnded (id %d, hook saw next id %d)", env.ID, idAtHook)
		}
	}
	if idAtHook == 0 {
		t.Fatal("OnTurnEnd not called")
	}
}

func TestRunTurnCancelledBeforeStartEndsCancelled(t *testing.T) {
	e, _ := echoEngine(t, [][]provider.StreamEvent{textReply()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.RunTurn(ctx, ids.NewClientID(), SimpleInput("hi")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if te := turnEnds(t, e.Bus); len(te) != 1 || te[0].Reason != "cancelled" {
		t.Fatalf("TurnEnded = %+v", te)
	}
}

func TestToolResultTextIsCappedPerBlock(t *testing.T) {
	big := strings.Repeat("x", 100<<10)
	out := CapToolResultContent([]control.ContentBlock{{Type: "text", Text: big}, {Type: "text", Text: "small"}})
	head, rest, _ := strings.Cut(out[0].Text, "\n")
	if head != big[:64<<10] || !strings.Contains(rest, "engine-capped") {
		t.Fatalf("100 KiB block not cut at 64 KiB with a notice (len %d)", len(out[0].Text))
	}
	if out[1].Text != "small" {
		t.Fatalf("small block changed: %q", out[1].Text)
	}
}

func TestDispatchMiddlewareErrorBecomesErrorResult(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), staticSource{name: "t", tools: []tool.Tool{fakeTool{name: "Echo"}}}); err != nil {
		t.Fatal(err)
	}
	fail := func(context.Context, tool.ToolCall, tool.EventSink, tool.Handler) (*tool.ToolResult, error) {
		return nil, errors.New("sandbox failed")
	}
	res, err := NewDispatcher(bus, reg, fail).Dispatch(context.Background(), ids.NewClientID(), tool.ToolCall{ID: ids.NewCallID(), Name: "Echo", Input: json.RawMessage(`{}`)})
	if err != nil || res == nil || !res.IsError || !strings.Contains(res.Content[0].Text, "sandbox failed") {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestChainRequestFirstIsOutermost(t *testing.T) {
	tag := func(s string) RequestMiddleware {
		return func(ctx context.Context, req *provider.Request, next RequestHandler) (<-chan provider.StreamEvent, error) {
			req.System += s
			return next(ctx, req)
		}
	}
	var got *provider.Request
	h := ChainRequest(captureRequest(&got), tag("1"), tag("2"), tag("3"))
	if _, err := h(context.Background(), &provider.Request{}); err != nil {
		t.Fatal(err)
	}
	if got.System != "123" {
		t.Fatalf("System = %q, want 123", got.System)
	}
}

func TestSubAgentEventsStayOutOfParentReplay(t *testing.T) {
	parent := NewBus(ids.NewSessionID())
	defer parent.Close()
	sub := NewSubBus(parent, "agent-1")
	defer sub.Close()
	if _, err := sub.Publish(control.TextDelta{Text: "x"}, ids.NewClientID()); err != nil {
		t.Fatal(err)
	}
	if got := parent.Backlog(context.Background(), 0, 0); len(got) != 0 {
		t.Fatalf("parent replay holds sub-agent events: %+v", got)
	}
}

type depthTool struct{ got chan int }

func (depthTool) Name() string        { return "Probe" }
func (depthTool) Description() string { return "probe" }
func (depthTool) Mutating() bool      { return false }
func (depthTool) InputSchema() []byte { return []byte(`{}`) }
func (d depthTool) Run(ctx context.Context, _ tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	d.got <- tool.SpawnDepthFromContext(ctx)
	return &tool.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: "ok"}}}, nil
}

func TestSpawnIncrementsSpawnDepth(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	probe := depthTool{got: make(chan int, 1)}
	if err := reg.Register(context.Background(), staticSource{name: "p", tools: []tool.Tool{probe}}); err != nil {
		t.Fatal(err)
	}
	call := []provider.StreamEvent{
		{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "c", Name: "Probe"}},
		{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
		{Kind: provider.KindToolUseStop},
		{Kind: provider.KindStop, FinishReason: "tool_use"},
	}
	e := NewEngine(session, bus, &fakeProvider{scripts: [][]provider.StreamEvent{call, textReply()}}, "m", NewDispatcher(bus, reg))
	if _, err := e.Spawn(context.Background(), "", "go"); err != nil {
		t.Fatal(err)
	}
	if d := <-probe.got; d != 1 {
		t.Fatalf("spawn depth inside a sub-agent = %d, want 1", d)
	}
}

type hangingProvider struct{ fakeProvider }

func (*hangingProvider) Chat(ctx context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

func TestRunTurnCancelledMidStreamEndsCancelled(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	e := NewEngine(session, bus, &hangingProvider{}, "m", NewDispatcher(bus, tool.NewRegistry()))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_ = e.RunTurn(ctx, ids.NewClientID(), SimpleInput("hi"))
	if te := turnEnds(t, bus); len(te) != 1 || te[0].Reason != "cancelled" {
		t.Fatalf("TurnEnded = %+v", te)
	}
}

func TestPermissionZeroTimeoutWaitsForLateAnswer(t *testing.T) {
	ch := make(chan PermissionAnswer, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		ch <- PermissionAnswer{Allow: true, Scope: control.ScopeOnce}
	}()
	_, invoked := runPermCall(t, tool.ToolCall{ID: ids.NewCallID(), Name: "Bash", Input: []byte(`{"cmd":"rm -rf /tmp/x"}`)}, stubRouter{ch: ch}, 0, nil)
	if !invoked {
		t.Fatal("timeout 0 denied an answer that came after 50ms")
	}
}

func TestPermissionPolicyDenyNeverRuns(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	eng := permission.New([]permission.Rule{{Tool: "Bash", Action: permission.DecisionDeny}})
	invoked := false
	res, err := PermissionMiddleware(bus, eng, nil, bus.Session(), time.Minute)(context.Background(),
		tool.ToolCall{ID: ids.NewCallID(), Name: "Bash", Input: []byte(`{"cmd":"ls"}`)}, nil,
		func(context.Context, tool.ToolCall, tool.EventSink) (*tool.ToolResult, error) {
			invoked = true
			return &tool.ToolResult{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	assertDenied(t, res, invoked)
}
