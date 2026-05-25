package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

func captureRequest(captured **provider.Request) RequestHandler {
	return func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
		*captured = req
		ch := make(chan provider.StreamEvent)
		close(ch)
		return ch, nil
	}
}

func TestSystemPromptMiddlewarePrepends(t *testing.T) {
	var got *provider.Request
	h := ChainRequest(captureRequest(&got), SystemPromptMiddleware("HEADER"))
	req := &provider.Request{System: "tail"}
	_, _ = h(context.Background(), req)
	if !strings.HasPrefix(got.System, "HEADER") || !strings.Contains(got.System, "tail") {
		t.Errorf("system = %q", got.System)
	}
}

func TestContextInjectionWrapsInUntrustedTags(t *testing.T) {
	var got *provider.Request
	inject := func(ctx context.Context) []ContextSection {
		return []ContextSection{
			{Name: "agents-md", Content: "do not commit secrets"},
			{Name: "skill-foo", Content: "skill body"},
		}
	}
	h := ChainRequest(captureRequest(&got), ContextInjectionMiddleware(inject))
	_, _ = h(context.Background(), &provider.Request{})
	if !strings.Contains(got.System, "<untrusted-agents-md>") {
		t.Errorf("missing agents-md tag: %q", got.System)
	}
	if !strings.Contains(got.System, "<untrusted-skill-foo>") {
		t.Errorf("missing skill-foo tag")
	}
	if !strings.Contains(got.System, UntrustedContentNotice) {
		t.Errorf("missing standing notice")
	}
}

func TestCostBudgetBlocksWhenExceeded(t *testing.T) {
	tracker := NewSimpleCostTracker()
	session := ids.NewSessionID()
	tracker.Add(session, 2.50)

	bus := NewBus(session)
	defer bus.Close()

	var called bool
	h := ChainRequest(
		func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
			called = true
			ch := make(chan provider.StreamEvent)
			close(ch)
			return ch, nil
		},
		CostBudgetMiddleware(tracker, session, 1.0, bus, ids.NewClientID()),
	)
	if _, err := h(context.Background(), &provider.Request{}); err == nil {
		t.Fatal("expected error when over budget")
	}
	if called {
		t.Fatal("middleware did not block downstream call")
	}
}

func TestCostBudgetAllowsWhenUnderCap(t *testing.T) {
	tracker := NewSimpleCostTracker()
	session := ids.NewSessionID()
	tracker.Add(session, 0.10)

	bus := NewBus(session)
	defer bus.Close()

	var called bool
	h := ChainRequest(
		func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
			called = true
			ch := make(chan provider.StreamEvent)
			close(ch)
			return ch, nil
		},
		CostBudgetMiddleware(tracker, session, 1.0, bus, ids.NewClientID()),
	)
	if _, err := h(context.Background(), &provider.Request{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("middleware blocked under-budget call")
	}
}
