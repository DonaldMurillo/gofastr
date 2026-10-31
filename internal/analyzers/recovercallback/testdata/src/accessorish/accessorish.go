// Package accessorish pins the accessor leg (2026-10-27 bug bash R9): a
// host hook kept in an atomic.Pointer and read through a zero-argument
// accessor is the same callback as the field it guards. island.Manager's
// heartbeat called `cb := m.presenceCallback(); cb(t)` bare on its
// goroutine, and the rule saw only a named function call.
package accessorish

import (
	"sync/atomic"
	"time"
)

type M struct {
	hook       atomic.Pointer[func(string)]
	plain      func(string)
	cancelLoop func()
}

func (m *M) SetHook(fn func(string)) { m.hook.Store(&fn) }

// hookFn is the atomic accessor: it dereferences the stored pointer.
func (m *M) hookFn() func(string) {
	if p := m.hook.Load(); p != nil {
		return *p
	}
	return nil
}

// plainFn returns the field itself.
func (m *M) plainFn() func(string) { return m.plain }

// built returns a closure the package made: not host code. Quiet.
func (m *M) built() func(string) {
	return func(string) {}
}

// pair returns the field beside another result.
func (m *M) pair() (bool, func(string)) { return m.plain != nil, m.plain }

// clocked returns the field before a selector that is no field: the
// field still decides.
func (m *M) clocked() (func(string), func() time.Time) { return m.plain, time.Now }

// own is the package's own method; bound returns its method value. Quiet.
func (m *M) own(string) {}

func (m *M) bound() func(string) { return m.own }

// stopper returns the package's own cancel func, plumbing the way a
// copied cancel-named field is. Quiet.
func (m *M) stopper() func() { return m.cancelLoop }

// lazy's inner return answers for the literal, not for lazy, which
// returns a closure of its own. Quiet.
func (m *M) lazy() func(string) {
	pick := func() func(string) { return m.plain }
	_ = pick
	return func(string) {}
}

func (m *M) Start() { go m.loop() }

func (m *M) loop() {
	for {
		<-time.After(time.Second)
		m.beat()
	}
}

func (m *M) beat() {
	cb := m.hookFn()
	if cb != nil {
		cb("t") // want `recovercallback: cb is invoked with no recover in scope`
	}
	pf := m.plainFn()
	if pf != nil {
		pf("t") // want `recovercallback: pf is invoked with no recover in scope`
	}
	if ok, pc := m.pair(); ok {
		pc("t") // want `recovercallback: pc is invoked with no recover in scope`
	}
	if kc, now := m.clocked(); now != nil {
		kc("t") // want `recovercallback: kc is invoked with no recover in scope`
	}
	own := m.built()
	own("t")
	bf := m.bound()
	bf("t")
	sf := m.stopper()
	sf()
	lz := m.lazy()
	lz("t")
	m.guarded()
}

// guarded reads the same accessor under a recover. Quiet.
func (m *M) guarded() {
	defer func() { _ = recover() }()
	if cb := m.hookFn(); cb != nil {
		cb("t")
	}
}
