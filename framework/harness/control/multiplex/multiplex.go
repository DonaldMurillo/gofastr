// Package multiplex implements the multi-client routing layer that
// sits between transports and the engine. Per hard rule 7, this is
// where "engine knows about clients, not transports" lives.
//
// Responsibilities:
//
//   - Track all attached clients per session.
//   - Total-order SendInput across all transports (mid-turn input
//     rejected with TurnInProgress; rule 9).
//   - Track originator for each turn so PermissionRequested events
//     carry the right ClientID.
//   - Enforce permission arbitration: agents cannot self-approve
//     (rule 11); at least one human ack required by default.
//   - Broadcast events to all attached clients of a session.
//   - Route AnswerPermission deliveries to the engine.
package multiplex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Mux is the multiplexer. One per harness process; manages multiple
// EngineRuns simultaneously.
type Mux struct {
	mu      sync.RWMutex
	engines map[ids.SessionID]*engineState
	now     func() time.Time
}

type engineState struct {
	engine    *engine.Engine
	clientsMu sync.RWMutex
	clients   map[ids.ClientID]control.Client

	// Total-ordering: turnBusy is atomic; turnOriginator is set when busy.
	turnBusy       atomic.Bool
	turnOriginator ids.ClientID

	// Permission arbitration: pending permission calls awaiting answers.
	pendingMu    sync.Mutex
	pendingPerms map[ids.CallID]*pendingPermission
}

type pendingPermission struct {
	originator ids.ClientID
	answerCh   chan engine.PermissionAnswer
	// AutoApprove disables the human-ack requirement (the
	// --auto-approve flag's effect on this prompt only).
	autoApprove bool
}

// New returns an empty Mux.
func New() *Mux {
	return &Mux{
		engines: make(map[ids.SessionID]*engineState),
		now:     time.Now,
	}
}

// RegisterEngine binds an EngineRun to the multiplexer. Subsequent
// Attach calls for the same session can connect clients to it.
func (m *Mux) RegisterEngine(e *engine.Engine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engines[e.Session] = &engineState{
		engine:       e,
		clients:      make(map[ids.ClientID]control.Client),
		pendingPerms: make(map[ids.CallID]*pendingPermission),
	}
}

// UnregisterEngine removes a session from the mux. Attached clients
// receive SessionEnded as the engine shuts down separately.
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

// Attach registers a client with the mux for one session. The client
// will receive broadcast events and can issue commands via Dispatch.
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

// Detach removes a client. Non-destructive at the engine level — the
// EngineRun continues, events still flow to other attached clients.
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

// Dispatch is the single entry point for client → engine commands.
// All transports call this; the mux enforces total ordering, identity
// rules, and routes the command.
//
// Returns an error event Reason on failure (caller wraps as
// control.Error and sends to the originating client).
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
	// Other commands (CreateSession, AttachSession, SetModel, etc.)
	// are handled by the harness composition layer; the mux only
	// arbitrates the session-scoped per-turn commands above.
	return ErrUnhandledCommand
}

func (m *Mux) handleSendInput(_ context.Context, c control.Client, cmd control.SendInput) error {
	st, ok := m.engines[cmd.SessionID]
	if !ok {
		return ErrUnknownSession
	}
	// Acquire turn slot. If already busy, reject with TurnInProgress.
	if !st.turnBusy.CompareAndSwap(false, true) {
		return &TurnInProgressError{OriginatorID: st.turnOriginator}
	}
	st.turnOriginator = c.ID()
	// Use the engine's CancelTree as the parent context for the
	// turn — NOT the caller's ctx. The caller's ctx may be a
	// short-lived HTTP request context that would cancel the turn
	// the instant the POST handler returns. Engine.Tree honors
	// CancelTurn properly.
	turnCtx := st.engine.Tree.Context()
	go func() {
		defer func() {
			st.turnOriginator = ""
			st.turnBusy.Store(false)
		}()
		_ = st.engine.RunTurn(turnCtx, c.ID(), cmd.Content)
	}()
	return nil
}

func (m *Mux) handleCancel(ctx context.Context, c control.Client, cmd control.CancelTurn) error {
	st, ok := m.engines[cmd.SessionID]
	if !ok {
		return ErrUnknownSession
	}
	// Use the engine's CancelTree (per § Lifecycle/cancellation).
	if st.engine.Tree != nil {
		st.engine.Tree.Cancel(context.Canceled)
	}
	return nil
}

// handleAnswer enforces identity-class rules and routes the answer to
// the engine's pending permission middleware.
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
	// Identity-class rules:
	//   - Agents cannot self-approve their own turn (rule 11).
	//   - Other agents can answer if AutoApprove is set; otherwise a
	//     human is required.
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

// Subscribe implements engine.AnswerRouter — the permission middleware
// calls Subscribe to wait for answers.
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
		originator: st.turnOriginator,
		answerCh:   ch,
	}
	st.pendingMu.Unlock()
	return ch
}

// Unsubscribe is called by the middleware when the permission resolves
// (allowed, denied, or timed out).
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

// Errors. These are wire-format Reason codes for control.Error events.
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

// engineState's mu is the per-session lock guarding clients map. The
// pendingPerms map has its own lock for tighter granularity.
func (s *engineState) mu() *sessionLock {
	return &sessionLock{s: s}
}

type sessionLock struct{ s *engineState }

func (l *sessionLock) Lock()    { l.s.clientsMu.Lock() }
func (l *sessionLock) Unlock()  { l.s.clientsMu.Unlock() }
func (l *sessionLock) RLock()   { l.s.clientsMu.RLock() }
func (l *sessionLock) RUnlock() { l.s.clientsMu.RUnlock() }
