package main

// =============================================================================
// demoSessionStore — the bounded per-visitor map behind the interactive demos.
//
// Concrete and unexported on purpose. An earlier draft made this a generic
// core/store.BoundedMap, but a review found it cleanly served only this one
// consumer: the other in-tree maps that look similar (the SSE stream registry,
// the rate-limiter buckets, the idempotency store) each need semantics this
// doesn't have — reject-on-full admission rather than LRU eviction, or an
// atomic mutate-under-lock the get-or-create model can't express. So the map +
// list + janitor lives here, specialized to string -> *demoState, and a shared
// package waits for a real second consumer with matching semantics.
//
// Two eviction axes, both load-bearing on a public origin:
//   - max: a hard LRU cap. A flood of cookie-minting requests evicts the
//     least-recently-used session rather than growing memory without bound.
//   - ttl: idle expiry swept by a background janitor — reclaims sessions from
//     visitors who left.
// =============================================================================

import (
	"container/list"
	"sync"
	"time"
)

type demoEntry struct {
	key      string
	val      *demoState
	lastSeen time.Time
}

type demoSessionStore struct {
	mu       sync.Mutex
	items    map[string]*list.Element // key -> *list.Element (Value = *demoEntry)
	order    *list.List               // front = most-recently-used, back = LRU
	max      int
	ttl      time.Duration
	clock    func() time.Time
	stop     chan struct{}
	stopOnce sync.Once
}

// newDemoSessionStoreBare builds the store WITHOUT starting the janitor. The
// real constructor and the deterministic tests share it — tests then swap in a
// fake clock before any goroutine reads it, so there's no clock data race.
func newDemoSessionStoreBare(max int, ttl time.Duration) *demoSessionStore {
	return &demoSessionStore{
		items: make(map[string]*list.Element),
		order: list.New(),
		max:   max,
		ttl:   ttl,
		clock: time.Now,
		stop:  make(chan struct{}),
	}
}

// newDemoSessionStore builds the store and starts the idle-expiry janitor when
// ttl > 0. The site's global never needs Close — it lives for the process.
func newDemoSessionStore(max int, ttl time.Duration) *demoSessionStore {
	s := newDemoSessionStoreBare(max, ttl)
	if ttl > 0 {
		go s.janitor(ttl / 2)
	}
	return s
}

// getOrCreate returns the session for key, seeding a fresh one if absent.
// Accessing a key refreshes its LRU position and TTL. Used by the write path
// (RPC handlers), which mints the cookie.
func (s *demoSessionStore) getOrCreate(key string) *demoState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		e := el.Value.(*demoEntry)
		e.lastSeen = s.clock()
		s.order.MoveToFront(el)
		return e.val
	}
	e := &demoEntry{key: key, val: seedDemoState(), lastSeen: s.clock()}
	s.items[key] = s.order.PushFront(e)
	s.evictOverflowLocked()
	return e.val
}

// get returns the session for key without creating one. Used by the SSR read
// path so a crawler or first load never populates the store. A hit refreshes
// LRU + TTL.
func (s *demoSessionStore) get(key string) (*demoState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*demoEntry)
	e.lastSeen = s.clock()
	s.order.MoveToFront(el)
	return e.val, true
}

// clear drops every session. The e2e reset helpers call it for isolation.
func (s *demoSessionStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]*list.Element)
	s.order.Init()
}

// length returns the live session count.
func (s *demoSessionStore) length() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// evictOverflowLocked removes LRU entries until within max. Caller holds mu.
func (s *demoSessionStore) evictOverflowLocked() {
	if s.max <= 0 {
		return
	}
	for len(s.items) > s.max {
		el := s.order.Back()
		if el == nil {
			break
		}
		e := el.Value.(*demoEntry)
		s.order.Remove(el)
		delete(s.items, e.key)
	}
}

// sweepExpired removes every entry idle beyond ttl. Split out from the janitor
// so tests drive expiry with a fake clock instead of sleeping.
func (s *demoSessionStore) sweepExpired() {
	if s.ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.clock().Add(-s.ttl)
	for el := s.order.Back(); el != nil; {
		prev := el.Prev()
		e := el.Value.(*demoEntry)
		if e.lastSeen.After(cutoff) {
			// order is MRU-front / LRU-back, so once one entry is newer than
			// the cutoff every entry ahead of it is too.
			break
		}
		s.order.Remove(el)
		delete(s.items, e.key)
		el = prev
	}
}

func (s *demoSessionStore) janitor(every time.Duration) {
	if every < time.Second {
		every = time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweepExpired()
		}
	}
}

func (s *demoSessionStore) close() { s.stopOnce.Do(func() { close(s.stop) }) }
