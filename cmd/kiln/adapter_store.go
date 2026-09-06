package main

import (
	"errors"
	"log"
	"sync"
)

// Cancellation causes propagated through context.WithCancelCause so the
// turn goroutine can render the right (superseded) note.
var (
	errSupersededByNewMessage = errors.New("superseded by newer message")
	errAgentSwitched          = errors.New("agent harness switched mid-turn")
)

// turnCancel is one in-flight registration of a turn's cancel hook. The
// pointer doubles as the ownership token ClearTurnCancel compares against
// the store's current slot: a turn goroutine that finished late (its turn
// was already superseded) must not orphan whatever hook owns the slot now.
type turnCancel struct {
	fn func(cause error)
}

// invokeCancel calls one turn's cancel hook outside the store lock. The
// hook is a context.CancelCauseFunc in production, but the store accepts
// any func(error) (tests install channel-sending fakes), and it fires on
// the watcher goroutine — a panicking callback must not take the process
// down with it.
func invokeCancel(c *turnCancel, cause error) {
	if c == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("kiln: agent turn cancel hook panicked: %v", r)
		}
	}()
	c.fn(cause)
}

// cancel hook for the in-flight turn (if any). The watcher reads from
// it on every chat_user event; the HTTP /kiln/agent endpoint mutates
// it. Concurrent-safe.
//
// Empty Adapter (zero value) means "no agent runs"; equivalent to
// --agent none. The watcher silently no-ops in that case.
//
// Set() cancels any in-flight turn: switching agents mid-session is
// a hard supersede (the running subprocess gets SIGKILL'd, the
// goroutine journals "(superseded by agent harness switch)"). Callers
// surface this in the UI before applying.
type AdapterStore struct {
	mu       sync.Mutex
	cur      Adapter
	current  *turnCancel
	inFlight bool
}

func NewAdapterStore(initial Adapter) *AdapterStore {
	return &AdapterStore{cur: initial}
}

func (s *AdapterStore) Get() Adapter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Set replaces the adapter. If a turn is in flight, cancel it with
// errAgentSwitched so the goroutine knows why and emits the right note.
func (s *AdapterStore) Set(a Adapter) {
	s.mu.Lock()
	prev := s.current
	s.current = nil
	s.inFlight = false
	s.cur = a
	s.mu.Unlock()
	if prev != nil {
		invokeCancel(prev, errAgentSwitched)
	}
}

// InFlight reports whether a turn is currently running.
func (s *AdapterStore) InFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight
}

// SetTurnCancel registers the cancel func for a turn the watcher just
// started and returns the registration's ownership token. If a turn is
// already in flight, it is cancelled first (errSupersededByNewMessage).
// The starting goroutine MUST pass the token to ClearTurnCancel: the
// superseded turn's own drain then clears nothing, because its token no
// longer owns the slot.
func (s *AdapterStore) SetTurnCancel(c func(error)) *turnCancel {
	reg := &turnCancel{fn: c}
	s.mu.Lock()
	prev := s.current
	s.current = reg
	s.inFlight = true
	s.mu.Unlock()
	invokeCancel(prev, errSupersededByNewMessage)
	return reg
}

// ClearTurnCancel marks the in-flight turn as finished, but only when
// the caller still owns the slot: a turn that was superseded (a newer
// message registered its own hook, an agent switch cleared the slot)
// leaves the live registration alone. Without the ownership check, the
// stale goroutine's unconditional clear nils the LIVE turn's hook — the
// stop button answers false and a later turn runs concurrently with the
// runaway one against the same world.
func (s *AdapterStore) ClearTurnCancel(token *turnCancel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == nil || s.current != token {
		return
	}
	s.current = nil
	s.inFlight = false
}

// errCancelledByUser is the cause used when the user clicks the
// header stop button while a turn is in flight.
var errCancelledByUser = errors.New("cancelled by user")

// CancelInFlight cancels the running turn (if any) without changing
// the adapter. Used by the panel's stop button. No-op if no turn is
// running.
func (s *AdapterStore) CancelInFlight() bool {
	s.mu.Lock()
	cur := s.current
	s.current = nil
	s.inFlight = false
	s.mu.Unlock()
	if cur != nil {
		invokeCancel(cur, errCancelledByUser)
		return true
	}
	return false
}
