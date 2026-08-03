package hook

import (
	"context"
	"fmt"
	"sync"
)

// HookType enumerates the lifecycle hook points for entity operations.
type HookType int

const (
	BeforeCreate HookType = iota
	AfterCreate
	BeforeUpdate
	AfterUpdate
	BeforeDelete
	AfterDelete
	BeforeList
	AfterList
	BeforeGet
	AfterGet
)

// HookFunc is the signature for a lifecycle hook.
// The data argument varies by hook type (e.g. map[string]any for create/update, string ID for delete).
// Return an error to cancel the operation (for Before* hooks) or log the failure (for After* hooks).
type HookFunc func(ctx context.Context, data any) error

// HookRegistry stores lifecycle hooks grouped by hook type.
//
// Registration is normally a setup-time activity, but two things read a
// registry on the request path — the CRUD handler's own hook lookups, and
// ?include= resolving a CHILD entity's registry — while kiln's build-mode
// runtime registers hooks against a live server. An unguarded map would make
// that pairing a concurrent read/write, which is an unrecoverable runtime
// throw rather than a panic a hook recover() could catch.
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[HookType][]HookFunc
	// label is the entity this registry belongs to, set by the framework
	// so a hook firing can be attributed in coverage. See SetLabel.
	label string
}

// NewHookRegistry creates an empty HookRegistry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

// RegisterHook appends a hook function for the given hook type.
// Hooks execute in registration order.
func (hr *HookRegistry) RegisterHook(hookType HookType, fn HookFunc) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.hooks[hookType] = append(hr.hooks[hookType], fn)
}

// ExecuteHooks runs all registered hooks for the given type in registration
// order. It stops on the first error and returns it. A panic inside a hook
// is caught and surfaced as an error — without recovery a single buggy or
// third-party hook would tear down the entire request goroutine.
func (hr *HookRegistry) ExecuteHooks(ctx context.Context, hookType HookType, data any) error {
	// Snapshot under the lock, then run unlocked: a hook may register another
	// hook, and holding the lock across arbitrary user code would deadlock.
	hr.mu.RLock()
	fns := append([]HookFunc(nil), hr.hooks[hookType]...)
	label := hr.label
	hr.mu.RUnlock()
	if len(fns) == 0 {
		// Nothing registered. Reporting a firing here would credit every
		// entity with full hook coverage the moment it served one request.
		return nil
	}
	if fn := observer.Load(); fn != nil {
		(*fn)(Firing{Entity: label, Type: hookType})
	}
	for _, fn := range fns {
		if err := runHookSafely(ctx, fn, data); err != nil {
			return err
		}
	}
	return nil
}

// runHookSafely calls fn with a deferred recover. Recovered panics become
// errors so the framework's lifecycle (tx rollback, error chain) handles
// them deterministically instead of unwinding the http stack.
func runHookSafely(ctx context.Context, fn HookFunc, data any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hook panic: %v", r)
		}
	}()
	return fn(ctx, data)
}

// HooksFor returns a copy of the hooks registered for the given type (for inspection/testing).
func (hr *HookRegistry) HooksFor(hookType HookType) []HookFunc {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	out := make([]HookFunc, len(hr.hooks[hookType]))
	copy(out, hr.hooks[hookType])
	return out
}
