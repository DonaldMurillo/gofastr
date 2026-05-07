package signal

import (
	"reflect"
	"sync"
)

// Computed is a derived signal that automatically recomputes when its
// dependencies change. Dependencies are tracked automatically when the
// compute function calls Signal.Get() on other signals.
type Computed[T any] struct {
	mu          sync.RWMutex
	value       T
	compute     func() T
	deps        []func() // unsubscribe functions for dependencies
	subscribers []func(T)
}

// NewComputed creates a computed signal that derives its value from other signals.
// The compute function is called immediately to get the initial value,
// and again whenever any dependency changes.
func NewComputed[T any](compute func() T) *Computed[T] {
	c := &Computed[T]{
		compute: compute,
	}

	// First evaluation with dependency tracking
	stop := startTracking()
	c.value = compute()
	deps := stop()

	// Subscribe to all dependencies
	for dep := range deps {
		if sub, ok := dep.(Subscribable); ok {
			unsub := sub.subscribeInternal(c.recompute)
			c.deps = append(c.deps, unsub)
		}
	}

	return c
}

// recompute recalculates the value and notifies subscribers if it changed.
func (c *Computed[T]) recompute() {
	c.mu.Lock()
	old := c.value
	c.value = c.compute()
	newVal := c.value
	subs := make([]func(T), len(c.subscribers))
	copy(subs, c.subscribers)
	c.mu.Unlock()

	if reflect.DeepEqual(old, newVal) {
		return
	}

	for _, fn := range subs {
		if fn != nil {
			fn(newVal)
		}
	}
}

// Get returns the current value. If there's an active tracking context,
// this computed is recorded as a dependency.
func (c *Computed[T]) Get() T {
	c.mu.RLock()
	v := c.value
	c.mu.RUnlock()
	trackDependency(c)
	return v
}

// Value returns the current value. It is an alias for Get().
func (c *Computed[T]) Value() T {
	return c.Get()
}

// Subscribe registers a listener that is called whenever the computed value changes.
// Returns an unsubscribe function.
func (c *Computed[T]) Subscribe(fn func(T)) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscribers = append(c.subscribers, fn)
	idx := len(c.subscribers) - 1
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.subscribers[idx] = nil
	}
}

// subscribeInternal implements Subscribable. It wraps the untyped callback
// so the Computed can be tracked as a dependency of other Computed/Effect.
func (c *Computed[T]) subscribeInternal(fn func()) func() {
	return c.Subscribe(func(T) { fn() })
}

// Dispose unsubscribes from all dependencies, stopping future recomputation.
func (c *Computed[T]) Dispose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, unsub := range c.deps {
		unsub()
	}
	c.deps = nil
}
