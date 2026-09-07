// Package e holds the discard arm of laxcoerce: `x, _ := p.(T)` on a
// caller-supplied any parameter, reduced from framework/typed_hooks.go
// OnBeforeDelete/OnAfterDelete (2026-09-07 round), with the postures
// the arm stays quiet on.
package e

import "context"

type registry struct{ name string }

func (r *registry) RegisterHook(kind string, fn func(ctx context.Context, data any) error) {
	_ = fn // stand-in for the real registration
}

func ctxUses(string) bool { return false }

// onBeforeDelete reduces typed_hooks.go: a non-string delete payload
// asserted with the ok discarded runs the hook for record id "".
func onBeforeDelete(r *registry, name string, fn func(ctx context.Context, id string) error) {
	r.RegisterHook("before-delete", func(ctx context.Context, data any) error {
		id, _ := data.(string) // want `comma-ok assertion on data \(a caller-supplied any\) discards the ok`
		return fn(ctx, id)
	})
}

// onAfterDeleteFixed is the fix posture: the failed assertion is an
// error, not a coerced zero.
func onAfterDeleteFixed(r *registry, name string, fn func(ctx context.Context, id string) error) {
	r.RegisterHook("after-delete", func(ctx context.Context, data any) error {
		id, ok := data.(string)
		if !ok {
			return &wrongPayloadError{got: data}
		}
		return fn(ctx, id)
	})
}

type wrongPayloadError struct{ got any }

func (e *wrongPayloadError) Error() string { return "payload is not a string" }

// ---------- the quiet postures -------------------------------------------

// unusedValue: the discarded ok's value also dies — dead code, not a
// silent coercion.
func unusedValue(data any) {
	id, _ := data.(string)
	_ = id
}

// fieldReceiver: the receiver is a struct field, not a parameter —
// developer data (the yamlBool posture).
type node struct{ Value any }

func yamlBool(n *node) bool {
	b, _ := n.Value.(bool)
	return b
}

// mapValueReceiver: the receiver is a map value, not a parameter (the
// paramString posture; the map arm's own territory).
func paramString(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// typedReceiver: a non-empty-interface parameter — a typed receiver
// cannot hold an arbitrary wrong type, and the arm's any-only contract
// holds.
type payload interface{ isPayload() }

type strPayload string

func (strPayload) isPayload() {}

func typedReceiver(p payload) string {
	s, _ := p.(strPayload)
	return string(s)
}

// ifaceTarget: asserting TO an interface yields nil, a different
// failure class.
func ifaceTarget(data any, r *registry) {
	m, _ := data.(interface{ isPayload() })
	_ = m
}

// closureCapturedOuter: the receiver is the OUTER function's parameter
// captured by a closure — a parameter of the checked function only.
func outer(data any, r *registry) {
	r.RegisterHook("kind", func(ctx context.Context, other any) error {
		s, _ := data.(string)
		_ = ctxUses(s)
		return nil
	})
}
