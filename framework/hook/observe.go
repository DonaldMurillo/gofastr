package hook

import "sync/atomic"

// Firing is one ExecuteHooks call that ran at least one registered hook.
type Firing struct {
	// Entity is the registry's label, empty when unlabelled.
	Entity string
	// Type is the lifecycle point.
	Type HookType
}

var observer atomic.Pointer[func(Firing)]

// SetObserver installs (nil clears) a process-wide callback run inline for each Firing; keep it cheap and concurrency-safe.
func SetObserver(fn func(Firing)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

// SetLabel names the entity a registry belongs to, for Firing.Entity.
func (hr *HookRegistry) SetLabel(label string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.label = label
}

// Label returns the registry's entity name, "" when unlabelled.
func (hr *HookRegistry) Label() string {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.label
}

// String names a hook type in lower case ("beforecreate"), as coverage manifests spell it.
func (h HookType) String() string {
	switch h {
	case BeforeCreate:
		return "beforecreate"
	case AfterCreate:
		return "aftercreate"
	case BeforeUpdate:
		return "beforeupdate"
	case AfterUpdate:
		return "afterupdate"
	case BeforeDelete:
		return "beforedelete"
	case AfterDelete:
		return "afterdelete"
	case BeforeList:
		return "beforelist"
	case AfterList:
		return "afterlist"
	case BeforeGet:
		return "beforeget"
	case AfterGet:
		return "afterget"
	default:
		return "unknown"
	}
}
