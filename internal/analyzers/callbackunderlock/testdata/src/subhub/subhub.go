// Package subhub is a NOVEL instantiation of the callback-under-lock
// shape: a pub/sub fan-out registry (no such code exists in this repo)
// invoking subscriber callbacks from a map while holding the hub
// mutex, plus the postures the rule keeps quiet on.
package subhub

import (
	"context"
	"sync"
)

type Hub struct {
	mu   sync.Mutex
	subs map[string]func(ev string)
}

// badFanout calls a map-element callback under the lock. Same shape as
// the registry gates, different container.
func (h *Hub) badFanout(ev string) {
	h.mu.Lock()
	for _, sub := range h.subs {
		if sub != nil {
			sub(ev) // want `callbackunderlock: sub is called while h.mu is held`
		}
	}
	h.mu.Unlock()
}

// goodFanout snapshots the callbacks under the lock and invokes them
// after release.
func (h *Hub) goodFanout(ev string) {
	h.mu.Lock()
	subs := make([]func(ev string), 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		if sub != nil {
			sub(ev)
		}
	}
}

// namedMethodCall is quiet: next-serve and named functions may take
// the lock themselves, which is a different bug this rule cannot see.
func (h *Hub) namedMethodCall() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.compact()
	notifyListeners(ev0)
}

// cancelFuncField is quiet: context.CancelFunc never re-enters user
// code, so holding the mutex across it cannot deadlock on a callback
// that re-takes the lock (moduleproto's cancel slots).
type slot struct{ cancel context.CancelFunc }

func (h *Hub) cancelFuncField(slots []slot) {
	h.mu.Lock()
	for _, s := range slots {
		s.cancel()
	}
	h.mu.Unlock()
}

func (h *Hub) compact() {}

var ev0 string

func notifyListeners(ev string) {}
