package style

import "sync"

var (
	registryMu sync.RWMutex
	registry   []func(*StyleSheet)
)

// Contribute queues fn to be applied to the host's stylesheet during the next
// [Apply] call.
func Contribute(fn func(*StyleSheet)) struct{} {
	if fn == nil {
		return struct{}{}
	}
	registryMu.Lock()
	registry = append(registry, fn)
	registryMu.Unlock()
	return struct{}{}
}

// ContributedCount reports how many fragments have been Contribute'd so far.
func ContributedCount() int {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return len(registry)
}

// Apply runs every [Contribute]'d fn against ss, in registration order.
func Apply(ss *StyleSheet) {
	if ss == nil {
		return
	}
	registryMu.RLock()
	fns := make([]func(*StyleSheet), len(registry))
	copy(fns, registry)
	registryMu.RUnlock()
	for _, fn := range fns {
		fn(ss)
	}
}

// ResetRegistryForTest empties the registry.
func ResetRegistryForTest() {
	registryMu.Lock()
	registry = nil
	registryMu.Unlock()
}
