package framework

import (
	"context"
	"sync"
	"testing"
)

// Shutdown racing start-hook setup is data-race free.
//
// appCtx/appCancel are mutated by runStartHooks (start path) and by
// Shutdown (teardown path). A SIGTERM-driven Shutdown can race Start's
// pre-listen lifecycle-context setup. The sibling `server` field is
// guarded by serverMu; appCtx/appCancel must be guarded too. Run under
// `go test -race` to surface the unguarded access.
func TestShutdownRacesStartHookSetup(t *testing.T) {
	const iterations = 50
	for i := 0; i < iterations; i++ {
		app := NewApp(WithoutDefaultMiddleware())

		var wg sync.WaitGroup
		wg.Add(2)

		// Start path: establishes the lifecycle context + cancel.
		go func() {
			defer wg.Done()
			_ = app.runStartHooks()
		}()

		// Teardown path: reads appCancel, calls it, nils it.
		go func() {
			defer wg.Done()
			_ = app.Shutdown(context.Background())
		}()

		wg.Wait()
	}
}
