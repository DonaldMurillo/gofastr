package main

import (
	"errors"
	"sync"
)

// Cancellation causes propagated through context.WithCancelCause so the
// turn goroutine can render the right (superseded) note.
var (
	errSupersededByNewMessage = errors.New("superseded by newer message")
	errAgentSwitched          = errors.New("agent harness switched mid-turn")
)

// AdapterStore holds the currently-selected agent Adapter and the
// cancel hook for the in-flight turn (if any). The watcher reads from
// it on every chat_user event; the HTTP /kiln/agent endpoint mutates
// it. Concurrent-safe.
//
// Empty Adapter (zero value) means "no agent runs"; equivalent to
// --agent none. The watcher silently no-ops in that case.
//
// Set() cancels any in-flight turn — switching agents mid-session is
// a hard supersede (the running subprocess gets SIGKILL'd, the
// goroutine journals "(superseded by agent harness switch)"). Callers
// surface this in the UI before applying.
type AdapterStore struct {
	mu        sync.Mutex
	cur       Adapter
	cancelFn  func(cause error)
	inFlight  bool
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
	prev := s.cancelFn
	s.cancelFn = nil
	s.inFlight = false
	s.cur = a
	s.mu.Unlock()
	if prev != nil {
		prev(errAgentSwitched)
	}
}

// InFlight reports whether a turn is currently running.
func (s *AdapterStore) InFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight
}

// SetTurnCancel registers the cancel func for a turn the watcher just
// started. If a turn is already in flight, it is cancelled first
// (errSupersededByNewMessage).
func (s *AdapterStore) SetTurnCancel(c func(error)) {
	s.mu.Lock()
	prev := s.cancelFn
	s.cancelFn = c
	s.inFlight = true
	s.mu.Unlock()
	if prev != nil {
		prev(errSupersededByNewMessage)
	}
}

// ClearTurnCancel marks the in-flight turn as finished. Safe to call
// even after Set() has already cleared it.
func (s *AdapterStore) ClearTurnCancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelFn = nil
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
	cancel := s.cancelFn
	s.cancelFn = nil
	s.inFlight = false
	s.mu.Unlock()
	if cancel != nil {
		cancel(errCancelledByUser)
		return true
	}
	return false
}
