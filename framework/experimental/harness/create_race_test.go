package harness

import (
	"errors"
	"sync"
	"testing"
)

// CreateSession racing Shutdown either lands in the list Shutdown tears
// down or is refused: no session is left registered afterwards.
func TestCreateRacingShutdownLeavesNone(t *testing.T) {
	for range 20 {
		h := newShutdownTestHarness(t)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var bad error
		var mu sync.Mutex
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := h.CreateSession(h.Providers[0], "openrouter:test"); err != nil && !errors.Is(err, ErrShutdown) {
					mu.Lock()
					bad = err
					mu.Unlock()
				}
			}()
		}
		close(start)
		_ = h.Shutdown()
		wg.Wait()
		if bad != nil {
			t.Fatalf("CreateSession: %v", bad)
		}
		h.sessionMu.Lock()
		n := len(h.sessionRuns)
		h.sessionMu.Unlock()
		if n != 0 {
			t.Fatalf("%d session(s) registered after Shutdown returned, want none", n)
		}
	}
}
