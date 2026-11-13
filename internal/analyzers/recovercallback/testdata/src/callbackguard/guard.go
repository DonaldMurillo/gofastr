package callbackguard

import "context"

func LogPanic(context.Context, string) { // want LogPanic:"direct recover guard"
	if recovered := recover(); recovered != nil {
		_ = recovered
	}
}

func NestedGuard() {
	defer func() { _ = recover() }()
}

func WrappedGuard() {
	LogPanic(context.Background(), "not a direct recover")
}

// Conditional recovers only when asked: a deferred Conditional(false)
// contains nothing, so it exports no guard fact.
func Conditional(enabled bool) {
	if enabled {
		_ = recover()
	}
}

// Rethrow recovers and panics again: the panic still escapes.
func Rethrow() {
	if recovered := recover(); recovered != nil {
		panic(recovered)
	}
}

// Assigned recovers in a plain statement first: a guard.
func Assigned() { // want Assigned:"direct recover guard"
	recovered := recover()
	_ = recovered
}

// DeferredPanic recovers, then a deferred call panics on the way out.
func DeferredPanic() {
	defer panic("cleanup")
	_ = recover()
}

// LiteralPanic recovers, then its deferred literal panics.
func LiteralPanic() {
	_ = recover()
	defer func() { panic("boom") }()
}

// ShortCircuit evaluates recover only when enabled.
func ShortCircuit(enabled bool) {
	_ = enabled && recover() != nil
}

// EarlyReturn may leave before it recovers.
func EarlyReturn(skip bool) {
	if skip {
		return
	}
	_ = recover()
}
