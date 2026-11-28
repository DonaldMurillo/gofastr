package main

import (
	"errors"
	"log"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var (
	errSupersededByNewMessage = errors.New("superseded by newer message")
	errAgentSwitched          = errors.New("agent harness switched mid-turn")
)

type turnCancel struct {
	fn func(cause error)
}

func invokeCancel(c *turnCancel, cause error) {
	if c == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("kiln: agent turn cancel hook panicked: %s", textsafe.Recovered(r))
		}
	}()
	c.fn(cause)
}

// AdapterStore holds the adapter the next turn runs and the in-flight
// turn's cancel hook. It is safe for concurrent use.
type AdapterStore struct {
	mu       sync.Mutex
	cur      Adapter
	current  *turnCancel
	inFlight bool
}

// NewAdapterStore returns a store holding initial.
func NewAdapterStore(initial Adapter) *AdapterStore {
	return &AdapterStore{cur: initial}
}

// Get returns the current adapter.
func (s *AdapterStore) Get() Adapter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Set replaces the adapter and cancels an in-flight turn with errAgentSwitched.
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

// SetTurnCancel registers a new turn's cancel hook, cancels the previous
// turn with errSupersededByNewMessage, and returns the token the turn passes
// to ClearTurnCancel.
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

// ClearTurnCancel ends the turn token registered, if it still owns the slot.
func (s *AdapterStore) ClearTurnCancel(token *turnCancel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == nil || s.current != token {
		return
	}
	s.current = nil
	s.inFlight = false
}

var errCancelledByUser = errors.New("cancelled by user")

// CancelInFlight cancels the running turn with errCancelledByUser, keeping
// the adapter, and reports whether there was one.
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
