package multiplex

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// blockingProvider holds its first Chat open until the turn's context
// ends; every later call answers "ok" and stops. With failChat the
// first Chat itself returns the context's error, the way an HTTP
// provider cancelled while connecting does; without it the stream
// closes, the way one cancelled mid-stream does.
type blockingProvider struct {
	fakeProvider
	failChat bool
}

func (b *blockingProvider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	b.mu.Lock()
	idx := b.calls
	b.calls++
	b.mu.Unlock()
	ch := make(chan provider.StreamEvent, 2)
	if idx == 0 && b.failChat {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if idx == 0 {
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	}
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "ok"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// turnEnd waits for the next TurnEnded on sub and returns its reason.
func turnEnd(t *testing.T, sub <-chan control.EventEnvelope) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case env := <-sub:
			if env.Kind != "TurnEnded" {
				continue
			}
			ev, err := control.DecodeEvent(env)
			if err != nil {
				t.Fatal(err)
			}
			return ev.(control.TurnEnded).Reason
		case <-deadline:
			t.Fatal("no TurnEnded")
			return ""
		}
	}
}

// A CancelTurn ends the turn it lands on as "cancelled" and no other:
// the turn after it, and a turn after a cancel sent while idle, run to
// completion.
func TestCancelEndsOnlyThatTurn(t *testing.T) {
	t.Run("mid-stream", func(t *testing.T) { cancelOneTurn(t, false) })
	t.Run("while connecting", func(t *testing.T) { cancelOneTurn(t, true) })
}

func cancelOneTurn(t *testing.T, failChat bool) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	prov := &blockingProvider{failChat: failChat}
	e := engine.NewEngine(session, bus, prov, "m", engine.NewDispatcher(bus, tool.NewRegistry()))
	m := New()
	m.RegisterEngine(e)
	c := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	_ = m.Attach(session, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.Subscribe(ctx)
	send := func(text string) {
		t.Helper()
		var err error
		for i := 0; i < 50; i++ {
			err = m.Dispatch(ctx, c, control.SendInput{SessionID: session, Content: engine.SimpleInput(text)})
			if _, busy := err.(*TurnInProgressError); !busy {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("send %q: %v", text, err)
		}
	}
	cancelTurn := func() {
		t.Helper()
		if err := m.Dispatch(ctx, c, control.CancelTurn{SessionID: session}); err != nil {
			t.Fatal(err)
		}
	}

	send("first")
	time.Sleep(50 * time.Millisecond)
	cancelTurn()
	if r := turnEnd(t, sub); r != "cancelled" {
		t.Fatalf("cancelled turn ended %q, want cancelled", r)
	}
	send("second")
	if r := turnEnd(t, sub); r != "complete" {
		t.Fatalf("turn after a cancel ended %q, want complete", r)
	}
	cancelTurn() // idle: nothing to cancel
	send("third")
	if r := turnEnd(t, sub); r != "complete" {
		t.Fatalf("turn after an idle cancel ended %q, want complete", r)
	}
}
