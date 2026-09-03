// Package wnparam pins the one-shot exemption's shape (review finding
// B2): the exemption covers a callback on the SAME object as the mutex
// under a deferred release — never a foreign object's callback merely
// because it is rooted at a parameter of the enclosing function. A
// blocking callback on a foreign object still wedges THIS registry's
// lock no matter how the release is spelled.
package wnparam

import "sync"

type Entry struct {
	Name string
	Gate func() error
}

type Reg struct {
	mu sync.Mutex
}

// oneShotPlain: a foreign callback under an explicit release. Fires.
func (r *Reg) oneShotPlain(e *Entry) error {
	r.mu.Lock()
	err := e.Gate() // want `callbackunderlock: e\.Gate is called while r\.mu is held`
	r.mu.Unlock()
	return err
}

// oneShotDeferred: the same foreign callback, deferred release. The
// exemption never covered a foreign object; it fires too.
func (r *Reg) oneShotDeferred(e *Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return e.Gate() // want `callbackunderlock: e\.Gate is called while r\.mu is held`
}

// sameObjectOneShot is the sanctioned shape: the callback belongs to
// the same object as the mutex, called once, under a deferred release
// (battery/setup's run-one-step-under-lock). Quiet.
type selfreg struct {
	mu   sync.Mutex
	gate func() error
}

func (s *selfreg) sameObjectOneShot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gate()
}
