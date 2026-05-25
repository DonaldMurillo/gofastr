package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/permission"
)

// fakeTool is a minimal tool for integration testing.
type fakeTool struct {
	name     string
	mutating bool
	out      string
}

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake" }
func (f fakeTool) InputSchema() []byte { return []byte(`{"type":"object"}`) }
func (f fakeTool) Mutating() bool      { return f.mutating }
func (f fakeTool) Run(ctx context.Context, c tool.ToolCall, sink tool.EventSink) (*tool.ToolResult, error) {
	return &tool.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: f.out}}}, nil
}

type staticSource struct {
	name  string
	tools []tool.Tool
}

func (s staticSource) Name() string                               { return s.name }
func (s staticSource) Tools(ctx context.Context) ([]tool.Tool, error) { return s.tools, nil }

func TestDispatchPublishesStartAndResult(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), staticSource{
		name: "test", tools: []tool.Tool{fakeTool{name: "Echo", mutating: false, out: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(bus, reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.Subscribe(ctx)

	originator := ids.NewClientID()
	res, err := d.Dispatch(context.Background(), originator, tool.ToolCall{
		ID:    ids.NewCallID(),
		Name:  "Echo",
		Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}

	// Expect ToolCallStarted then ToolResult.
	kinds := drain(sub, 200*time.Millisecond)
	if len(kinds) < 2 || kinds[0] != "ToolCallStarted" || kinds[1] != "ToolResult" {
		t.Fatalf("event order = %v, want [ToolCallStarted ToolResult …]", kinds)
	}
}

type routerStub struct {
	answer PermissionAnswer
}

func (r *routerStub) Subscribe(_ ids.SessionID, _ ids.CallID) <-chan PermissionAnswer {
	ch := make(chan PermissionAnswer, 1)
	ch <- r.answer
	return ch
}

func (r *routerStub) Unsubscribe(_ ids.SessionID, _ ids.CallID) {}

func TestPermissionMiddlewareAllow(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	_ = reg.Register(context.Background(), staticSource{
		name: "test", tools: []tool.Tool{fakeTool{name: "DangerTool", mutating: true, out: "ok"}},
	})

	pe := permission.New(nil)
	router := &routerStub{answer: PermissionAnswer{Allow: true, Scope: control.ScopeOnce}}
	mw := PermissionMiddleware(bus, pe, router, session, 500*time.Millisecond)

	d := NewDispatcher(bus, reg, mw)
	res, err := d.Dispatch(context.Background(), ids.NewClientID(), tool.ToolCall{
		ID: ids.NewCallID(), Name: "DangerTool", Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("expected allow, got error: %+v", res)
	}
}

func TestPermissionMiddlewareDenyOnTimeout(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	_ = reg.Register(context.Background(), staticSource{
		name: "test", tools: []tool.Tool{fakeTool{name: "Danger", mutating: true, out: "ok"}},
	})
	pe := permission.New(nil)
	mw := PermissionMiddleware(bus, pe, nil, session, 50*time.Millisecond)

	d := NewDispatcher(bus, reg, mw)
	res, err := d.Dispatch(context.Background(), ids.NewClientID(), tool.ToolCall{
		ID: ids.NewCallID(), Name: "Danger", Input: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected timeout to deny, got: %+v", res)
	}
}

func drain(ch <-chan control.EventEnvelope, after time.Duration) []string {
	var kinds []string
	deadline := time.After(after)
	for {
		select {
		case env := <-ch:
			kinds = append(kinds, env.Kind)
		case <-deadline:
			return kinds
		}
	}
}
