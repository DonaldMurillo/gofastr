package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
)

type stubSink struct {
	progress []string
	events   []control.Event
}

func (s *stubSink) EmitProgress(p string)     { s.progress = append(s.progress, p) }
func (s *stubSink) EmitEvent(e control.Event) { s.events = append(s.events, e) }

func TestChainOrder(t *testing.T) {
	var trace []string
	mkMW := func(label string) Middleware {
		return func(ctx context.Context, c ToolCall, sink EventSink, next Handler) (*ToolResult, error) {
			trace = append(trace, "before:"+label)
			r, err := next(ctx, c, sink)
			trace = append(trace, "after:"+label)
			return r, err
		}
	}
	base := func(ctx context.Context, c ToolCall, sink EventSink) (*ToolResult, error) {
		trace = append(trace, "base")
		return &ToolResult{}, nil
	}
	h := Chain(base, mkMW("a"), mkMW("b"), mkMW("c"))
	if _, err := h(context.Background(), ToolCall{}, &stubSink{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"before:a", "before:b", "before:c", "base", "after:c", "after:b", "after:a"}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("trace[%d] = %q, want %q\nfull: %v", i, trace[i], want[i], trace)
		}
	}
}

func TestChainPropagatesError(t *testing.T) {
	want := errors.New("boom")
	mw := func(ctx context.Context, c ToolCall, sink EventSink, next Handler) (*ToolResult, error) {
		return next(ctx, c, sink)
	}
	base := func(ctx context.Context, c ToolCall, sink EventSink) (*ToolResult, error) {
		return nil, want
	}
	h := Chain(base, mw)
	if _, err := h(context.Background(), ToolCall{}, &stubSink{}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
