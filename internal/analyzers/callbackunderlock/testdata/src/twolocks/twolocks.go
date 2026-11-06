// Package twolocks pins the two-lock judgment: with more than one
// mutex held, EVERY held lock is judged (acquisition order), and the
// exactly-once carve-out must hold for each of them — the callback
// reports for the first lock whose posture it breaks, whichever order
// the locks were taken in.
package twolocks

import "sync"

type Inner struct {
	mu      sync.Mutex
	onEvent func(ev string)
}

type Outer struct {
	mu sync.RWMutex
	in *Inner
}

// mixedRelease: the outer read lock's release is deferred and the
// callback shares its root, but the inner mutex is released by hand
// after the call — a panic in the callback skips it, so the call
// reports on the inner mutex.
func (o *Outer) mixedRelease(ev string) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	o.in.mu.Lock()
	o.in.onEvent(ev) // want `callbackunderlock: o\.in\.onEvent is called while o\.in\.mu is held`
	o.in.mu.Unlock()
}

type sameRoot struct {
	a       sync.Mutex
	b       sync.Mutex
	onEvent func(ev string)
}

// Both locks on the callback's own owner, both releases deferred,
// invoked at most once: the carve-out holds for every held lock —
// quiet.
func (s *sameRoot) carved(ev string) {
	s.a.Lock()
	defer s.a.Unlock()
	s.b.Lock()
	defer s.b.Unlock()
	s.onEvent(ev)
}
