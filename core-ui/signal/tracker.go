package signal

import (
	"runtime"
	"strconv"
	"sync"
)

// trackingContext holds the current dependency tracking scope.
// When a Signal.Get() or Computed.Get() is called inside a Computed or Effect,
// the signal is added to the current tracking context.
type trackingContext struct {
	dependencies map[any]struct{} // set of tracked signals (using interface{})
}

// Dependency tracking is goroutine-local: each goroutine maintains its own
// stack of active tracking scopes. A previous implementation used a single
// process-global *trackingContext, which let two goroutines building a
// Computed/Effect concurrently overwrite each other's scope — producing a
// data race on the dependency map and cross-goroutine dependency leakage
// (a Computed subscribing to another goroutine's signals). Keying by
// goroutine ID isolates the scopes so the package upholds its documented
// "safe for concurrent use" contract.
var (
	trackingMu sync.Mutex
	// trackingStacks maps a goroutine ID to its stack of active tracking
	// scopes. The top of each stack is the goroutine's current context.
	trackingStacks = make(map[uint64][]*trackingContext)
)

// startTracking begins a new tracking scope for the calling goroutine.
// Returns a function that stops tracking and returns the collected dependencies.
// The returned stop function must be called on the same goroutine.
func startTracking() func() map[any]struct{} {
	gid := goroutineID()
	ctx := &trackingContext{
		dependencies: make(map[any]struct{}),
	}

	trackingMu.Lock()
	trackingStacks[gid] = append(trackingStacks[gid], ctx)
	trackingMu.Unlock()

	return func() map[any]struct{} {
		trackingMu.Lock()
		stack := trackingStacks[gid]
		if n := len(stack); n > 0 {
			stack = stack[:n-1]
			if len(stack) == 0 {
				delete(trackingStacks, gid)
			} else {
				trackingStacks[gid] = stack
			}
		}
		trackingMu.Unlock()
		return ctx.dependencies
	}
}

// trackDependency adds a signal to the calling goroutine's current tracking
// context. Called automatically by Signal.Get() and Computed.Get().
func trackDependency(sig any) {
	gid := goroutineID()
	trackingMu.Lock()
	defer trackingMu.Unlock()
	stack := trackingStacks[gid]
	if n := len(stack); n > 0 {
		stack[n-1].dependencies[sig] = struct{}{}
	}
}

// goroutineID returns the ID of the calling goroutine. Go does not expose
// this directly, so it is parsed from the runtime stack header
// ("goroutine <id> [...]"). This is only used as an isolation key for
// tracking scopes, never as a stable external identifier.
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// Stack header format: "goroutine 123 [running]:\n..."
	s := buf[:n]
	const prefix = "goroutine "
	if len(s) < len(prefix) {
		return 0
	}
	s = s[len(prefix):]
	// Read digits up to the first space.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	id, _ := strconv.ParseUint(string(s[:i]), 10, 64)
	return id
}
