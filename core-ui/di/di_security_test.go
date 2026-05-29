package di

import (
	"sync"
	"testing"
)

// TestInjectConcurrentColdStart asserts that concurrent first-time
// injection of a func-provided dependency does not race on the
// container's internal maps (concurrent map writes are fatal & kill
// the process). Run with -race to also surface the data race.
func TestInjectConcurrentColdStart(t *testing.T) {
	type svc struct{ n int }
	type screen struct {
		S *svc `inject:""`
	}

	c := NewContainer()
	// Register as a func-constructor so the lazy branch in Inject runs
	// (a direct value would be pre-resolved and never write under RLock).
	if err := c.Provide(func() *svc { return &svc{n: 42} }); err != nil {
		t.Fatalf("Provide: %v", err)
	}

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			var sc screen
			if err := c.Inject(&sc); err != nil {
				t.Errorf("Inject: %v", err)
				return
			}
			if sc.S == nil || sc.S.n != 42 {
				t.Errorf("injected value wrong: %+v", sc.S)
			}
		}()
	}
	wg.Wait()
}
