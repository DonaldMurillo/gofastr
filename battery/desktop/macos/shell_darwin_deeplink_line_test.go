//go:build darwin && arm64

package macos

import (
	"sync"
	"testing"
	"time"
)

// TestDeepLinkLineKeepsArrivalOrder: two GetURL events reach the
// installed OnDeepLink one at a time, in the order they arrived. The
// second does not run while the first is still in the callback.
func TestDeepLinkLineKeepsArrivalOrder(t *testing.T) {
	prev := deepLinkOpen.Load()
	t.Cleanup(func() { deepLinkOpen.Store(prev) })
	var (
		mu    sync.Mutex
		order []string
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fn := func(raw string) {
		mu.Lock()
		order = append(order, raw)
		mu.Unlock()
		if raw == "notes://first" {
			once.Do(func() { close(entered) })
			<-release
		}
	}
	deepLinkOpen.Store(&fn)
	var l deepLinkLine
	l.push("notes://first")
	<-entered
	l.push("notes://second")
	time.Sleep(50 * time.Millisecond) // a goroutine per event would deliver now
	mu.Lock()
	early := len(order)
	mu.Unlock()
	close(release)
	if early != 1 {
		t.Fatalf("OnDeepLink ran %d times while the first call was still running, want 1", early)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		idle := !l.draining
		l.mu.Unlock()
		if idle || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "notes://first" || order[1] != "notes://second" {
		t.Fatalf("delivery order = %q, want [notes://first notes://second]", order)
	}
}
