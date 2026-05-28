package hook

import (
	"context"
	"fmt"
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
type HookRegistry struct {
	hooks map[HookType][]HookFunc
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
	hr.hooks[hookType] = append(hr.hooks[hookType], fn)
}

// ExecuteHooks runs all registered hooks for the given type in registration
// order. It stops on the first error and returns it. A panic inside a hook
// is caught and surfaced as an error — without recovery a single buggy or
// third-party hook would tear down the entire request goroutine.
func (hr *HookRegistry) ExecuteHooks(ctx context.Context, hookType HookType, data any) error {
	for _, fn := range hr.hooks[hookType] {
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
	out := make([]HookFunc, len(hr.hooks[hookType]))
	copy(out, hr.hooks[hookType])
	return out
}
