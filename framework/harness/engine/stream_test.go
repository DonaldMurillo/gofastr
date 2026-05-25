package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

func TestCollectStreamTextOnly(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	stream := make(chan provider.StreamEvent, 4)
	stream <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "Hello"}
	stream <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: " world"}
	stream <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(stream)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.Subscribe(ctx)
	originator := ids.NewClientID()

	done := make(chan StreamSummary, 1)
	go func() {
		s, err := CollectStream(context.Background(), bus, originator, stream)
		if err != nil {
			t.Errorf("CollectStream error: %v", err)
		}
		done <- s
	}()

	select {
	case summary := <-done:
		if summary.Text != "Hello world" {
			t.Errorf("text = %q, want 'Hello world'", summary.Text)
		}
		if summary.FinishReason != "stop" {
			t.Errorf("finish = %q", summary.FinishReason)
		}
	case <-time.After(time.Second):
		t.Fatal("CollectStream did not return")
	}

	// Validate two TextDelta events were broadcast.
	kinds := drain(sub, 100*time.Millisecond)
	count := 0
	for _, k := range kinds {
		if k == "TextDelta" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("got %d TextDelta events, want 2", count)
	}
}

func TestCollectStreamToolUse(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()

	stream := make(chan provider.StreamEvent, 5)
	stream <- provider.StreamEvent{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_x", Name: "Echo"}}
	stream <- provider.StreamEvent{Kind: provider.KindToolUseDelta, InputDelta: `{"text"`}
	stream <- provider.StreamEvent{Kind: provider.KindToolUseDelta, InputDelta: `:"hi"}`}
	stream <- provider.StreamEvent{Kind: provider.KindToolUseStop}
	stream <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "tool_use"}
	close(stream)

	summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.ToolUses) != 1 {
		t.Fatalf("got %d tool uses, want 1", len(summary.ToolUses))
	}
	tu := summary.ToolUses[0]
	if tu.Name != "Echo" || tu.ID != "call_x" {
		t.Errorf("tool use = %+v", tu)
	}
	if !strings.Contains(string(tu.Input), `"text":"hi"`) {
		t.Errorf("accumulated input = %q", string(tu.Input))
	}
}

func TestCollectStreamErrorAborts(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()
	stream := make(chan provider.StreamEvent, 2)
	stream <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "partial"}
	stream <- provider.StreamEvent{Kind: provider.KindError, Err: ErrStreamClosed}
	close(stream)

	summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), stream)
	if err == nil {
		t.Fatal("expected error")
	}
	if summary.Text != "partial" {
		t.Errorf("partial text not preserved: %q", summary.Text)
	}
}
