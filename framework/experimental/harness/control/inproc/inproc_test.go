package inproc

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: "pong"}
	ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	close(ch)
	return ch, nil
}
func (fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

func TestInprocSendInputReachesEngine(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	e := engine.NewEngine(session, bus, fakeProvider{}, "m", d)

	mux := multiplex.New()
	mux.RegisterEngine(e)

	c := New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	sub := c.Subscribe(ctx)

	if err := c.Send(context.Background(), control.SendInput{
		SessionID: session,
		Content:   engine.SimpleInput("ping"),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for at least one TextDelta plus TurnEnded.
	gotText := false
	gotEnd := false
	deadline := time.After(2 * time.Second)
	for !(gotText && gotEnd) {
		select {
		case env := <-sub:
			switch env.Kind {
			case "TextDelta":
				gotText = true
			case "TurnEnded":
				gotEnd = true
			}
		case <-deadline:
			t.Fatalf("timeout: gotText=%v gotEnd=%v", gotText, gotEnd)
		}
	}
}

func TestInprocCloseStopsSend(t *testing.T) {
	c := New(ids.NewClientID(), control.IdentityHuman, nil, nil)
	c.Close()
	if !c.IsClosed() {
		t.Fatal("expected IsClosed after Close")
	}
}
