// Package golaunch pins the go-launch posture (review finding B1): a
// callback launched on its own goroutine between Lock and Unlock does
// not run in the linear held region — the launch is non-blocking and
// the callback runs on its own stack after the region — so it is quiet
// exactly like a closure handed to `go`. The synchronous spelling
// beside it still fires.
package golaunch

import "sync"

type Hub struct {
	mu   sync.Mutex
	subs map[string]func(ev string)
	done func()
}

// asyncFanout launches each subscriber under the lock. Quiet.
func (h *Hub) asyncFanout(ev string) {
	h.mu.Lock()
	for _, sub := range h.subs {
		go sub(ev)
	}
	h.mu.Unlock()
}

// asyncDone launches a func field under the lock. Quiet.
func (h *Hub) asyncDone() {
	h.mu.Lock()
	go h.done()
	h.mu.Unlock()
}

// asyncDefer: the release is deferred and the callback is still only
// launched, never run, inside the region. Quiet.
func (h *Hub) asyncDefer() {
	h.mu.Lock()
	defer h.mu.Unlock()
	go h.done()
}

// syncFanout is the control: the same map callbacks invoked
// synchronously under the lock.
func (h *Hub) syncFanout(ev string) {
	h.mu.Lock()
	for _, sub := range h.subs {
		sub(ev) // want `callbackunderlock: sub is called while h\.mu is held`
	}
	h.mu.Unlock()
}
