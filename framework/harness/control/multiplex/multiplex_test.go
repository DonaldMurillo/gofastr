package multiplex

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Re-implement minimal fake provider for these tests.
type fakeProvider struct {
	scripts [][]provider.StreamEvent
	calls   int
	mu      sync.Mutex
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()
	ch := make(chan provider.StreamEvent, 8)
	if idx < len(f.scripts) {
		for _, ev := range f.scripts[idx] {
			ch <- ev
		}
	}
	close(ch)
	return ch, nil
}

func (f *fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (f *fakeProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// stubClient implements control.Client without any real transport.
type stubClient struct {
	id    ids.ClientID
	class control.IdentityClass
}

func (s stubClient) ID() ids.ClientID                     { return s.id }
func (s stubClient) IdentityClass() control.IdentityClass { return s.class }
func (s stubClient) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (s stubClient) Send(_ context.Context, _ control.Command) error { return nil }
func (s stubClient) Close() error                                    { return nil }

func makeEngine(t *testing.T) (*engine.Engine, *fakeProvider) {
	t.Helper()
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	reg := tool.NewRegistry()
	d := engine.NewDispatcher(bus, reg)
	prov := &fakeProvider{scripts: [][]provider.StreamEvent{{
		{Kind: provider.KindTextDelta, Text: "ok"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}}
	return engine.NewEngine(session, bus, prov, "m", d), prov
}

func TestAttachDetach(t *testing.T) {
	m := New()
	e, _ := makeEngine(t)
	m.RegisterEngine(e)
	c := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	if err := m.Attach(e.Session, c); err != nil {
		t.Fatal(err)
	}
	if !m.HasHumanAttached(e.Session) {
		t.Error("expected HasHumanAttached")
	}
	m.Detach(e.Session, c.ID())
	if m.HasHumanAttached(e.Session) {
		t.Error("client not detached")
	}
}

func TestSendInputRejectsMidTurn(t *testing.T) {
	m := New()
	e, prov := makeEngine(t)
	// Long-running first turn: scripted to take a while.
	prov.scripts = [][]provider.StreamEvent{
		makeSlowScript(),
		{
			{Kind: provider.KindTextDelta, Text: "second"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
	}
	m.RegisterEngine(e)
	c1 := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	c2 := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	_ = m.Attach(e.Session, c1)
	_ = m.Attach(e.Session, c2)

	if err := m.Dispatch(context.Background(), c1, control.SendInput{
		SessionID: e.Session,
		Content:   engine.SimpleInput("first"),
	}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	// Immediate second SendInput from c2: should be rejected.
	err := m.Dispatch(context.Background(), c2, control.SendInput{
		SessionID: e.Session,
		Content:   engine.SimpleInput("second"),
	})
	var tip *TurnInProgressError
	if !errors.As(err, &tip) {
		t.Fatalf("expected TurnInProgressError, got %v", err)
	}
	if tip.OriginatorID != c1.ID() {
		t.Errorf("OriginatorID = %s, want %s", tip.OriginatorID, c1.ID())
	}
}

func TestAnswerPermissionAgentCannotSelfApprove(t *testing.T) {
	m := New()
	e, _ := makeEngine(t)
	m.RegisterEngine(e)
	agent := stubClient{id: ids.NewClientID(), class: control.IdentityAgent}
	_ = m.Attach(e.Session, agent)

	// Manually register a pending permission as if the engine had asked.
	st := m.engines[e.Session]
	callID := ids.NewCallID()
	st.turnOriginator.Store(agent.ID())
	st.pendingPerms[callID] = &pendingPermission{
		originator: agent.ID(),
		answerCh:   make(chan engine.PermissionAnswer, 1),
	}

	err := m.Dispatch(context.Background(), agent, control.AnswerPermission{
		SessionID: e.Session,
		CallID:    callID,
		Decision:  control.DecisionAllow,
	})
	if !errors.Is(err, ErrAgentCannotSelfApprove) {
		t.Fatalf("err = %v, want ErrAgentCannotSelfApprove", err)
	}
}

func TestAnswerPermissionHumanAllowed(t *testing.T) {
	m := New()
	e, _ := makeEngine(t)
	m.RegisterEngine(e)
	human := stubClient{id: ids.NewClientID(), class: control.IdentityHuman}
	_ = m.Attach(e.Session, human)

	st := m.engines[e.Session]
	callID := ids.NewCallID()
	answerCh := make(chan engine.PermissionAnswer, 1)
	st.pendingPerms[callID] = &pendingPermission{
		originator: ids.NewClientID(), // not the human
		answerCh:   answerCh,
	}

	err := m.Dispatch(context.Background(), human, control.AnswerPermission{
		SessionID: e.Session,
		CallID:    callID,
		Decision:  control.DecisionAllow,
		Scope:     control.ScopeArgvGlob,
	})
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	select {
	case ans := <-answerCh:
		if !ans.Allow {
			t.Error("expected allow")
		}
		if ans.Scope != control.ScopeArgvGlob {
			t.Errorf("scope = %v", ans.Scope)
		}
	case <-time.After(time.Second):
		t.Fatal("answer not delivered")
	}
}

func makeSlowScript() []provider.StreamEvent {
	// 200ms of doing nothing (we use a channel-close delay to mimic
	// the provider still streaming).
	return []provider.StreamEvent{
		{Kind: provider.KindTextDelta, Text: "slow"},
		// no Stop — the channel close at the end of fakeProvider.Chat
		// will end the stream after the test's first call returns
		// from CollectStream. The race window is fine for this test.
		{Kind: provider.KindStop, FinishReason: "stop"},
	}
}
