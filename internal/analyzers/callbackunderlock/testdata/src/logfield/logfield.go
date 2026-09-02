// Package logfield pins the printf-logger posture (review finding B3):
// a func(format string, args ...any)-shaped field is logging plumbing,
// not an app callback — the sibling analyzer recovercallback already
// classifies those fields as infrastructure — so progress-logging
// inside a locked walk is quiet. A func field of any other shape under
// the lock still fires.
package logfield

import "sync"

type Reg struct {
	mu      sync.Mutex
	entries map[string]int
	logf    func(format string, args ...any)
	gate    func(name string) error
}

// flushed is the standard idiom: log each entry while walking the
// registry under its lock. Quiet.
func (r *Reg) flushed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.entries {
		r.logf("flushing %s", name)
	}
}

// gated: a non-printf func field invoked under the lock. Fires.
func (r *Reg) gated(name string) error {
	r.mu.Lock()
	err := r.gate(name) // want `callbackunderlock: r\.gate is called while r\.mu is held`
	r.mu.Unlock()
	return err
}
