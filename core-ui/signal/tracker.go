package signal

import "sync"

// trackingContext holds the current dependency tracking scope.
// When a Signal.Get() or Computed.Get() is called inside a Computed or Effect,
// the signal is added to the current tracking context.
type trackingContext struct {
	dependencies map[any]struct{} // set of tracked signals (using interface{})
}

var (
	trackingMu sync.Mutex
	currentCtx *trackingContext
)

// startTracking begins a new tracking scope.
// Returns a function that stops tracking and returns the collected dependencies.
func startTracking() func() map[any]struct{} {
	trackingMu.Lock()
	ctx := &trackingContext{
		dependencies: make(map[any]struct{}),
	}
	prev := currentCtx
	currentCtx = ctx
	trackingMu.Unlock()

	return func() map[any]struct{} {
		trackingMu.Lock()
		currentCtx = prev
		trackingMu.Unlock()
		return ctx.dependencies
	}
}

// trackDependency adds a signal to the current tracking context.
// Called automatically by Signal.Get() and Computed.Get().
func trackDependency(sig any) {
	trackingMu.Lock()
	defer trackingMu.Unlock()
	if currentCtx != nil {
		currentCtx.dependencies[sig] = struct{}{}
	}
}
