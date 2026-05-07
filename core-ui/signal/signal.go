package signal

import (
	"fmt"
	"reflect"
	"sync"
)

// Subscribable is implemented by Signal[T] and Computed[T].
// It allows generic subscription without knowing the concrete type parameter.
type Subscribable interface {
	subscribeInternal(fn func()) func()
}

// Signal is a reactive value container. When its value changes,
// all subscribed listeners are notified.
type Signal[T any] struct {
	mu          sync.RWMutex
	value       T
	subscribers []func(T)
}

// New creates a new Signal with the given initial value.
func New[T any](initial T) *Signal[T] {
	return &Signal[T]{
		value: initial,
	}
}

// Get returns the current value. If there's an active tracking context
// (inside a Computed or Effect), this signal is recorded as a dependency.
func (s *Signal[T]) Get() T {
	s.mu.RLock()
	v := s.value
	s.mu.RUnlock()
	trackDependency(s)
	return v
}

// Value returns the current value. It is an alias for Get().
func (s *Signal[T]) Value() T {
	return s.Get()
}

// Set updates the value and notifies all subscribers.
// If the value is unchanged (per reflect.DeepEqual), subscribers are NOT notified.
func (s *Signal[T]) Set(v T) {
	s.mu.Lock()
	old := s.value
	s.value = v
	subs := make([]func(T), len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	// Skip notification if value unchanged
	if isEqual(old, v) {
		return
	}

	notify := func() {
		for _, fn := range subs {
			if fn != nil {
				fn(v)
			}
		}
	}

	// In batch mode, defer notification
	if enqueueBatch(notify) {
		return
	}

	notify()
}

// Update applies a function to the current value and sets the result.
func (s *Signal[T]) Update(fn func(T) T) {
	s.mu.RLock()
	current := s.value
	s.mu.RUnlock()
	s.Set(fn(current))
}

// Subscribe registers a listener that is called whenever the value changes.
// Returns an unsubscribe function.
func (s *Signal[T]) Subscribe(fn func(T)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = append(s.subscribers, fn)
	idx := len(s.subscribers) - 1
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.subscribers[idx] = nil // nil out to preserve indices
	}
}

// subscribeInternal implements Subscribable. It wraps the untyped callback
// so the signal can be tracked as a dependency of Computed/Effect.
func (s *Signal[T]) subscribeInternal(fn func()) func() {
	return s.Subscribe(func(T) { fn() })
}

// String implements fmt.Stringer.
func (s *Signal[T]) String() string {
	return fmt.Sprintf("%v", s.Get())
}

// isEqual compares two values using reflect.DeepEqual.
func isEqual[T any](a, b T) bool {
	return reflect.DeepEqual(a, b)
}
