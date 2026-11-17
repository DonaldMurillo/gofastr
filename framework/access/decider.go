package access

import (
	"context"
	"net/http"
	"slices"
)

// Ref is the resource a check is about: Type is the entity name, ID the record
// id or "" for a collection-level check. The zero Ref names no resource.
type Ref struct {
	Type string
	ID   string
}

// RoleGateCapability is the capability role gates pass to the Decider, with the
// zero Ref. Resource-aware deciders should abstain on it.
const RoleGateCapability Permission = "auth:role-gate"

// Decision is a Decider's verdict. The zero value is DecisionAbstain.
type Decision int

const (
	// DecisionAbstain defers the check to the role policy.
	DecisionAbstain Decision = iota
	// DecisionAllow permits the check without the role policy.
	DecisionAllow
	// DecisionDeny refuses the check even when the role policy would allow it.
	DecisionDeny
)

// Decider is consulted before the role policy on every gated check, route
// gates included (with the zero Ref). A nil Decider falls through.
type Decider func(ctx context.Context, roles []string, capability Permission, resource Ref) Decision

type deciderKey struct{}

// WithDecider stores a Decider in the context.
func WithDecider(ctx context.Context, d Decider) context.Context {
	return context.WithValue(ctx, deciderKey{}, d)
}

// GetDecider returns the Decider in ctx, or nil.
func GetDecider(ctx context.Context) Decider {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(deciderKey{}).(Decider)
	return d
}

// CanResource checks capability on resource: a scope-restricted caller needs a
// scope for it, then the Decider decides, and on abstain Can does. A nil ctx is false.
func CanResource(ctx context.Context, capability Permission, resource Ref) bool {
	if ctx == nil {
		return false
	}
	if !ScopeAllows(ctx, capability) {
		return observe(ctx, capability, false, "can-resource")
	}
	if d, ok := ctx.Value(deciderKey{}).(Decider); ok && d != nil {
		switch d(ctx, GetRoles(ctx), capability, resource) {
		case DecisionAllow:
			return observe(ctx, capability, true, "can-resource")
		case DecisionDeny:
			return observe(ctx, capability, false, "can-resource")
		case DecisionAbstain:
		}
	}
	return Can(ctx, capability)
}

// CanResourceExact is CanResource where, on abstain, the roles must hold
// capability by name: Wildcard does not count.
func CanResourceExact(ctx context.Context, capability Permission, resource Ref) bool {
	if ctx == nil {
		return false
	}
	if !ScopeAllows(ctx, capability) {
		return observe(ctx, capability, false, "can-resource-exact")
	}
	if d, ok := ctx.Value(deciderKey{}).(Decider); ok && d != nil {
		switch d(ctx, GetRoles(ctx), capability, resource) {
		case DecisionAllow:
			return observe(ctx, capability, true, "can-resource-exact")
		case DecisionDeny:
			return observe(ctx, capability, false, "can-resource-exact")
		case DecisionAbstain:
		}
	}
	return observe(ctx, capability, slices.Contains(GetPermissions(ctx), capability), "can-resource-exact")
}

// requireResource is CanResource's decision without recording an evaluation.
func requireResource(ctx context.Context, capability Permission, resource Ref) bool {
	if d := GetDecider(ctx); d != nil {
		switch d(ctx, GetRoles(ctx), capability, resource) {
		case DecisionAllow:
			return true
		case DecisionDeny:
			return false
		}
	}
	policy, _ := ctx.Value(policyKey{}).(*RolePolicy)
	return policy != nil && policy.Can(ctx, capability)
}

// DeciderMiddleware installs d into each request's context.
func DeciderMiddleware(d Decider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithDecider(r.Context(), d)))
		})
	}
}
