package multiplex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

func TestOtherAgentNeedsHumanToAnswer(t *testing.T) {
	m := New()
	e, _ := makeEngine(t)
	m.RegisterEngine(e)
	other := stubClient{id: ids.NewClientID(), class: control.IdentityAgent}
	callID := ids.NewCallID()
	m.engines[e.Session].pendingPerms[callID] = &pendingPermission{
		originator: ids.NewClientID(),
		answerCh:   make(chan engine.PermissionAnswer, 1),
	}
	err := m.Dispatch(context.Background(), other, control.AnswerPermission{
		SessionID: e.Session, CallID: callID, Decision: control.DecisionAllow,
	})
	if !errors.Is(err, ErrHumanAnswerRequired) {
		t.Fatalf("err = %v, want ErrHumanAnswerRequired", err)
	}
}

func TestTurnOutlivesDispatchContext(t *testing.T) {
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	e := engine.NewEngine(session, bus, &blockingProvider{}, "m", engine.NewDispatcher(bus, tool.NewRegistry()))
	m := New()
	m.RegisterEngine(e)
	c := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Dispatch(ctx, c, control.SendInput{SessionID: session, Content: engine.SimpleInput("hi")}); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(200 * time.Millisecond)
	err := m.Dispatch(context.Background(), c, control.SendInput{SessionID: session, Content: engine.SimpleInput("again")})
	if _, busy := err.(*TurnInProgressError); !busy {
		t.Fatalf("turn ended with its dispatch context: second send err = %v", err)
	}
	_ = m.Dispatch(context.Background(), c, control.CancelTurn{SessionID: session})
}

func TestSlotFreeWhenTurnEndedArrives(t *testing.T) {
	m := New()
	e, prov := makeEngine(t)
	for range 1000 {
		prov.scripts = append(prov.scripts, prov.scripts[0])
	}
	m.RegisterEngine(e)
	c := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := e.Bus.Subscribe(ctx)
	for i := range 1000 {
		if err := m.Dispatch(ctx, c, control.SendInput{SessionID: e.Session, Content: engine.SimpleInput("go")}); err != nil {
			t.Fatalf("send %d right after TurnEnded: %v", i, err)
		}
		turnEnd(t, sub)
	}
}
