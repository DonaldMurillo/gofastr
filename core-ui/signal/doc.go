// Package signal provides a reactive signal system inspired by Angular signals
// and SolidJS. It offers three core primitives:
//
//   - Signal[T]: a reactive value container with Get/Set/Update/Subscribe
//   - Computed[T]: a derived signal that auto-recomputes when dependencies change
//   - Effect: a side-effect that re-runs when tracked signals change
//
// Dependency tracking is automatic — any Signal.Get() call made inside a
// Computed's compute function or an Effect's callback is recorded as a
// dependency and will trigger re-evaluation when changed.
//
// All primitives are safe for concurrent use.
package signal
