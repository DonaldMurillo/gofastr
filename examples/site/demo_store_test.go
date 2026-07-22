package main

import (
	"sync"
	"testing"
	"time"
)

// bareStore builds a store with a fixed clock and NO janitor goroutine, so TTL
// tests are deterministic and there's no unsynchronized clock read.
func bareStore(max int, ttl time.Duration, now *time.Time) *demoSessionStore {
	s := newDemoSessionStoreBare(max, ttl)
	s.clock = func() time.Time { return *now }
	return s
}

func TestDemoStoreGetOrCreateSeedsOnce(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := bareStore(0, 0, &now)

	a := s.getOrCreate("k")
	b := s.getOrCreate("k")
	if a != b {
		t.Fatal("getOrCreate returned different sessions for the same key")
	}
	if s.length() != 1 {
		t.Fatalf("length = %d, want 1", s.length())
	}
	// A fresh session starts at the seed: kanban present, create-next 4.
	if len(a.kanban) == 0 || a.createNext != 4 {
		t.Fatalf("session not seeded: kanban=%d createNext=%d", len(a.kanban), a.createNext)
	}
}

func TestDemoStoreGetMissing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := bareStore(0, 0, &now)
	if _, ok := s.get("nope"); ok {
		t.Fatal("get on absent key returned ok=true")
	}
}

func TestDemoStoreMaxEntriesEvictsLRU(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := bareStore(2, 0, &now)

	s.getOrCreate("a")
	s.getOrCreate("b")
	// Touch "a" so "b" is the LRU.
	if _, ok := s.get("a"); !ok {
		t.Fatal("a missing before eviction")
	}
	s.getOrCreate("c") // exceeds cap → evict LRU ("b")

	if s.length() != 2 {
		t.Fatalf("length = %d, want 2 after cap eviction", s.length())
	}
	if _, ok := s.get("b"); ok {
		t.Fatal("b survived; expected it evicted as the LRU entry")
	}
	if _, ok := s.get("a"); !ok {
		t.Fatal("a was evicted; expected it to survive (recently touched)")
	}
	if _, ok := s.get("c"); !ok {
		t.Fatal("c missing after insert")
	}
}

func TestDemoStoreTTLExpiry(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	now := base
	s := bareStore(0, time.Minute, &now)

	s.getOrCreate("a")

	now = base.Add(30 * time.Second)
	s.sweepExpired()
	if _, ok := s.get("a"); !ok {
		t.Fatal("a expired early")
	}

	// get above refreshed lastSeen to t+30s; advance past ttl from there.
	now = base.Add(30*time.Second + 2*time.Minute)
	s.sweepExpired()
	if _, ok := s.get("a"); ok {
		t.Fatal("a survived past its TTL")
	}
	if s.length() != 0 {
		t.Fatalf("length = %d, want 0 after expiry", s.length())
	}
}

func TestDemoStoreTTLTouchKeepsAlive(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	now := base
	s := bareStore(0, time.Minute, &now)

	s.getOrCreate("a")
	for i := 0; i < 10; i++ {
		now = now.Add(30 * time.Second)
		if _, ok := s.get("a"); !ok {
			t.Fatalf("a expired at step %d despite steady access", i)
		}
		s.sweepExpired()
	}
}

func TestDemoStoreClear(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := bareStore(0, 0, &now)
	s.getOrCreate("a")
	s.getOrCreate("b")
	s.clear()
	if s.length() != 0 {
		t.Fatalf("length = %d after clear, want 0", s.length())
	}
	if _, ok := s.get("a"); ok {
		t.Fatal("a present after clear")
	}
}

func TestDemoStoreConcurrentSingleSession(t *testing.T) {
	s := newDemoSessionStore(100, 0) // real clock, no janitor (ttl=0)
	var mu sync.Mutex
	seen := map[*demoState]bool{}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				p := s.getOrCreate("shared")
				mu.Lock()
				seen[p] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(seen) != 1 {
		t.Fatalf("concurrent getOrCreate produced %d distinct sessions, want 1", len(seen))
	}
}
