package hook

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
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

// HookFunc is a lifecycle hook: an error stops the chain, a write hook's error (After too) makes the write roll back, a read hook's error fails the read.
type HookFunc func(ctx context.Context, data any) error

// HookRegistry stores lifecycle hooks grouped by hook type, safe for concurrent use.
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[HookType][]HookFunc
	label string
}

// NewHookRegistry creates an empty HookRegistry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

// RegisterHook appends fn to the hooks for hookType; hooks run in registration order.
func (hr *HookRegistry) RegisterHook(hookType HookType, fn HookFunc) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.hooks[hookType] = append(hr.hooks[hookType], fn)
}

// ExecuteHooks runs the hooks for hookType in order and returns the first error, a panic as *PanicError.
func (hr *HookRegistry) ExecuteHooks(ctx context.Context, hookType HookType, data any) error {
	hr.mu.RLock()
	fns := append([]HookFunc(nil), hr.hooks[hookType]...)
	label := hr.label
	hr.mu.RUnlock()
	if len(fns) == 0 {
		return nil
	}
	if fn := observer.Load(); fn != nil {
		func() {
			defer textsafe.LogPanic(ctx, "hook: observer panicked; ignored")
			(*fn)(Firing{Entity: label, Type: hookType})
		}()
	}
	for _, fn := range fns {
		if err := runHookSafely(ctx, fn, data); err != nil {
			return err
		}
	}
	return nil
}

// PanicError is a hook's panic, a server error rather than a refusal; Error scrubs control bytes.
type PanicError struct {
	// Value is what the hook passed to panic.
	Value any
}

func (e *PanicError) Error() string { return "hook panic: " + textsafe.Recovered(e.Value) }

func runHookSafely(ctx context.Context, fn HookFunc, data any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().ErrorContext(ctx, "hook: lifecycle hook panicked; the operation fails",
				"panic", textsafe.Recovered(r),
				"stack", string(debug.Stack()),
			)
			err = &PanicError{Value: r}
		}
	}()
	return fn(ctx, data)
}

// HooksFor returns a copy of the hooks registered for hookType.
func (hr *HookRegistry) HooksFor(hookType HookType) []HookFunc {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	out := make([]HookFunc, len(hr.hooks[hookType]))
	copy(out, hr.hooks[hookType])
	return out
}
