package signal

import (
	"sync"
	"testing"
)

// TestConcurrentTrackingIsolated asserts that dependency tracking is
// goroutine-scoped: two goroutines each building a Computed/Effect must
// not share the active tracking context, so a Computed never subscribes
// to another goroutine's signals and the dependency map is never mutated
// concurrently. Run with -race to catch the global-currentCtx data race.
func TestConcurrentTrackingIsolated(t *testing.T) {
	t.Run("concurrent computed builds do not race or cross-link", func(t *testing.T) {
		const goroutines = 16
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func(n int) {
				defer wg.Done()
				// Each goroutine owns its own signal and computed.
				s := New(n)
				c := NewComputed(func() int { return s.Get() * 2 })
				if got := c.Get(); got != n*2 {
					t.Errorf("computed got %d, want %d", got, n*2)
				}
				// Mutating only this goroutine's signal must recompute
				// only this goroutine's computed.
				s.Set(n + 1)
				if got := c.Get(); got != (n+1)*2 {
					t.Errorf("after set: computed got %d, want %d", got, (n+1)*2)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("foreign signal read during another build is not captured", func(t *testing.T) {
		// foreign belongs to "another goroutine" and is read while this
		// goroutine is mid-build. With goroutine-local tracking it must
		// NOT become a dependency of c.
		foreign := New(0)
		own := New(1)

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 1000; i++ {
				_ = foreign.Get()
			}
		}()

		var c *Computed[int]
		built := make(chan struct{})
		go func() {
			close(start)
			c = NewComputed(func() int { return own.Get() })
			close(built)
		}()
		<-built
		wg.Wait()

		recomputed := false
		c.Subscribe(func(int) { recomputed = true })
		foreign.Set(999) // must not trigger c
		if recomputed {
			t.Fatal("computed recomputed from a foreign goroutine's signal — tracking leaked across goroutines")
		}
	})

	t.Run("concurrent effects do not race", func(t *testing.T) {
		const goroutines = 16
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				s := New(0)
				var mu sync.Mutex
				runs := 0
				dispose := Effect(func() {
					_ = s.Get()
					mu.Lock()
					runs++
					mu.Unlock()
				})
				s.Set(1)
				dispose()
			}()
		}
		wg.Wait()
	})
}
