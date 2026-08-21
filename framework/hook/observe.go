package hook

import "sync/atomic"

// Firing is one registered hook that actually ran.
//
// The distinction that matters is "ran" versus "was looked up".
// ExecuteHooks is called on every CRUD operation whether or not anything
// is registered, so recording the call would report full coverage for an
// app with no hooks at all. Only a hook with a body behind it counts.
type Firing struct {
	// Entity is the registry's label, the entity name the framework
	// created it for. Empty for a registry nobody labelled.
	Entity string
	// Type is the lifecycle point.
	Type HookType
}

// observer is the process-wide firing hook. An atomic pointer rather than
// a mutex because ExecuteHooks sits on the request path; an uninstalled
// observer costs one atomic load, which is what production pays.
//
// A hook rather than a direct import of the coverage recorder, for the
// same reason router.SetServeHook and access.SetObserver are: this is a
// leaf package and the recorder is test tooling.
var observer atomic.Pointer[func(Firing)]

// SetObserver installs a callback fired whenever a registered hook runs.
// Pass nil to clear. Intended for test tooling, the framework's semantic
// coverage recorder is the first consumer.
//
// The callback runs inline on the request path, so it must be cheap, must
// not block, and must tolerate concurrent calls.
func SetObserver(fn func(Firing)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

// SetLabel names the entity a registry belongs to, so a firing can be
// attributed. The framework calls this when it creates the per-entity
// registry; a hand-built registry can set it too.
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

// String names a hook type as it appears in coverage manifests and
// diagnostics, lower-case, matching the `OnBeforeCreate` API spelling.
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
