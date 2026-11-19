package multiplex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Mux routes every transport's commands to the session engines it holds, one per harness process.
type Mux struct {
	mu      sync.RWMutex
	engines map[ids.SessionID]*engineState
	now     func() time.Time
}

type engineState struct {
	engine    *engine.Engine
	clientsMu sync.RWMutex
	clients   map[ids.ClientID]control.Client

	turnBusy       atomic.Bool
	turnOriginator atomic.Value

	turnMu     sync.Mutex
	turnCancel context.CancelCauseFunc

	pendingMu    sync.Mutex
	pendingPerms map[ids.CallID]*pendingPermission
}

type pendingPermission struct {
	originator  ids.ClientID
	answerCh    chan engine.PermissionAnswer
	autoApprove bool
}

// New returns an empty Mux.
func New() *Mux {
	return &Mux{
		engines: make(map[ids.SessionID]*engineState),
		now:     time.Now,
	}
}

// RegisterEngine adds a session's engine so clients can Attach to it.
func (m *Mux) RegisterEngine(e *engine.Engine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engines[e.Session] = &engineState{
		engine:       e,
		clients:      make(map[ids.ClientID]control.Client),
		pendingPerms: make(map[ids.CallID]*pendingPermission),
	}
}

// UnregisterEngine removes a session; it does not stop the engine.
func (m *Mux) UnregisterEngine(session ids.SessionID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.engines, session)
}

// EngineFor returns the engine for a session, or nil.
func (m *Mux) EngineFor(session ids.SessionID) *engine.Engine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.engines[session]; ok {
		return s.engine
	}
	return nil
}

// Attach adds a client to a session; it errors on an unknown session or a client already attached.
func (m *Mux) Attach(session ids.SessionID, c control.Client) error {
	m.mu.RLock()
	st, ok := m.engines[session]
	m.mu.RUnlock()
	if !ok {
		return ErrUnknownSession
	}
	st.mu().Lock()
	defer st.mu().Unlock()
	if _, dup := st.clients[c.ID()]; dup {
		return errors.New("multiplex: client already attached")
	}
	st.clients[c.ID()] = c
	return nil
}

// Detach removes a client from a session; the engine keeps running.
func (m *Mux) Detach(session ids.SessionID, clientID ids.ClientID) {
	m.mu.RLock()
	st, ok := m.engines[session]
	m.mu.RUnlock()
	if !ok {
		return
	}
	st.mu().Lock()
	defer st.mu().Unlock()
	delete(st.clients, clientID)
}

// HasHumanAttached reports whether at least one human-class client
// is currently attached to the session.
func (m *Mux) HasHumanAttached(session ids.SessionID) bool {
	m.mu.RLock()
	st, ok := m.engines[session]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	st.mu().RLock()
	defer st.mu().RUnlock()
	for _, c := range st.clients {
		if c.IdentityClass() == control.IdentityHuman {
			return true
		}
	}
	return false
}

// Dispatch runs SendInput, CancelTurn and AnswerPermission and returns ErrUnhandledCommand for any other command.
func (m *Mux) Dispatch(ctx context.Context, c control.Client, cmd control.Command) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch v := cmd.(type) {
	case control.SendInput:
		return m.handleSendInput(ctx, c, v)
	case control.CancelTurn:
		return m.handleCancel(ctx, c, v)
	case control.AnswerPermission:
		return m.handleAnswer(ctx, c, v)
	}
	return ErrUnhandledCommand
}

func (m *Mux) handleSendInput(_ context.Context, c control.Client, cmd control.SendInput) error {
	st, ok := m.engines[cmd.SessionID]
	if !ok {
		return ErrUnknownSession
	}
	if !st.turnBusy.CompareAndSwap(false, true) {
		return &TurnInProgressError{OriginatorID: st.originatorID()}
	}
	st.turnOriginator.Store(c.ID())
	turnCtx, cancelTurn := context.WithCancelCause(st.engine.Tree.Context())
	st.turnMu.Lock()
	st.turnCancel = cancelTurn
	st.turnMu.Unlock()
	var released atomic.Bool
	release := func() {
		if released.CompareAndSwap(false, true) {
			st.turnOriginator.Store(ids.ClientID(""))
			st.turnBusy.Store(false)
		}
	}
	st.engine.OnTurnEnd = release
	go func() {
		defer cancelTurn(nil)
		defer release()
		_ = st.engine.RunTurn(turnCtx, c.ID(), cmd.Content)
	}()
	return nil
}

func (m *Mux) handleCancel(ctx context.Context, c control.Client, cmd control.CancelTurn) error {
	st, ok := m.engines[cmd.SessionID]
	if !ok {
		return ErrUnknownSession
	}
	st.turnMu.Lock()
	cancel := st.turnCancel
	st.turnMu.Unlock()
	if cancel != nil {
		cancel(context.Canceled)
	}
	return nil
}

func (m *Mux) handleAnswer(ctx context.Context, c control.Client, cmd control.AnswerPermission) error {
	st, ok := m.engines[cmd.SessionID]
	if !ok {
		return ErrUnknownSession
	}
	st.pendingMu.Lock()
	pend, ok := st.pendingPerms[cmd.CallID]
	st.pendingMu.Unlock()
	if !ok {
		return ErrNoPendingPermission
	}
	if c.ID() == pend.originator && c.IdentityClass() == control.IdentityAgent {
		return ErrAgentCannotSelfApprove
	}
	if !pend.autoApprove && c.IdentityClass() != control.IdentityHuman {
		return ErrHumanAnswerRequired
	}
	allow := cmd.Decision == control.DecisionAllow
	select {
	case pend.answerCh <- engine.PermissionAnswer{
		CallID: cmd.CallID, Allow: allow, Scope: cmd.Scope, Source: c.ID(),
	}:
		return nil
	default:
		return ErrAnswerChannelFull
	}
}

// Subscribe implements engine.AnswerRouter: it returns the channel a permission answer for callID arrives on.
func (m *Mux) Subscribe(session ids.SessionID, callID ids.CallID) <-chan engine.PermissionAnswer {
	m.mu.RLock()
	st, ok := m.engines[session]
	m.mu.RUnlock()
	if !ok {
		ch := make(chan engine.PermissionAnswer)
		close(ch)
		return ch
	}
	ch := make(chan engine.PermissionAnswer, 1)
	st.pendingMu.Lock()
	st.pendingPerms[callID] = &pendingPermission{
		originator: st.originatorID(),
		answerCh:   ch,
	}
	st.pendingMu.Unlock()
	return ch
}

// Unsubscribe implements engine.AnswerRouter: it drops the pending prompt for callID.
func (m *Mux) Unsubscribe(session ids.SessionID, callID ids.CallID) {
	m.mu.RLock()
	st, ok := m.engines[session]
	m.mu.RUnlock()
	if !ok {
		return
	}
	st.pendingMu.Lock()
	delete(st.pendingPerms, callID)
	st.pendingMu.Unlock()
}

// The Mux's refusals.
var (
	ErrUnknownSession         = errors.New("multiplex: unknown session")
	ErrNoPendingPermission    = errors.New("multiplex: no pending permission")
	ErrAgentCannotSelfApprove = errors.New("multiplex: agent cannot self-approve permission (rule 11)")
	ErrHumanAnswerRequired    = errors.New("multiplex: a human-class client must answer this prompt")
	ErrAnswerChannelFull      = errors.New("multiplex: permission answer channel full (race)")
	ErrUnhandledCommand       = errors.New("multiplex: command not handled at this layer")
)

// TurnInProgressError matches control.ReasonTurnInProgress on the wire.
type TurnInProgressError struct {
	OriginatorID ids.ClientID
}

func (e *TurnInProgressError) Error() string {
	return "multiplex: another client is sending input (originator " + string(e.OriginatorID) + ")"
}

func (s *engineState) mu() *sessionLock {
	return &sessionLock{s: s}
}

type sessionLock struct{ s *engineState }

func (l *sessionLock) Lock()    { l.s.clientsMu.Lock() }
func (l *sessionLock) Unlock()  { l.s.clientsMu.Unlock() }
func (l *sessionLock) RLock()   { l.s.clientsMu.RLock() }
func (l *sessionLock) RUnlock() { l.s.clientsMu.RUnlock() }

func (st *engineState) originatorID() ids.ClientID {
	if v := st.turnOriginator.Load(); v != nil {
		return v.(ids.ClientID)
	}
	return ""
}
