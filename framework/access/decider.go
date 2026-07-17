package access

import (
	"context"
	"net/http"
)

// Ref identifies the resource a capability check is about. Type is the entity
// name (e.g. "projects"); ID is the record id, or "" for a collection-level
// check (List, Create, a batch, or the SSE feed). The zero Ref carries no
// resource information — a decider that ignores empty Refs is effectively
// opting out of resource-awareness for that call.
type Ref struct {
	Type string
	ID   string
}

// Decision is the verdict a Decider returns for one resource-scoped capability
// check. The zero value is DecisionAbstain so a decider that forgets to return
// falls through to the role policy rather than silently allowing or denying.
type Decision int

const (
	// DecisionAbstain defers the check to the role policy (Can). Use it when
	// the decider has no opinion for this resource — e.g. the resource type is
	// not one it governs, or the caller holds a role it doesn't model.
	DecisionAbstain Decision = iota
	// DecisionAllow permits the check; the role policy is not consulted.
	DecisionAllow
	// DecisionDeny refuses the check even when the role policy would allow.
	DecisionDeny
)

// Decider is consulted before the role policy when a resource-aware check
// (CanResource) runs. roles is the caller's resolved roles — the same slice
// access.Middleware / WithRoles install. The decider receives the capability
// and the Ref under check so it can express rules the coarse role policy
// cannot ("a team maintainer may edit their team's projects").
//
// Return DecisionAbstain to fall through to Can. A nil/missing decider also
// falls through, so wiring a decider is strictly opt-in.
type Decider func(ctx context.Context, roles []string, capability Permission, resource Ref) Decision

// deciderKey is the context key for a Decider. It lives here, beside the
// Decider type, so the whole resource-scoped seam is one self-contained
// addition; access.go (policyKey, rolesKey) stays untouched.
type deciderKey struct{}

// WithDecider stores a Decider in the context, mirroring the WithPolicy /
// WithRoles pair. Downstream CanResource calls will consult it before the role
// policy. Installing a decider is strictly additive: with none in ctx,
// CanResource answers exactly what Can answers.
func WithDecider(ctx context.Context, d Decider) context.Context {
	return context.WithValue(ctx, deciderKey{}, d)
}

// GetDecider reads back the Decider installed via WithDecider (by
// DeciderMiddleware or an explicit WithDecider call). It is the reader half of
// the decider-context seam. Returns nil when ctx is nil or carries no decider
// — never panics.
func GetDecider(ctx context.Context) Decider {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(deciderKey{}).(Decider)
	return d
}

// CanResource is the resource-aware capability check. It consults a Decider
// installed in ctx (via WithDecider / DeciderMiddleware) BEFORE the role
// policy:
//
//   - DecisionAllow  → true (role policy not consulted)
//   - DecisionDeny   → false (role policy not consulted)
//   - DecisionAbstain → falls through to Can
//
// With no decider in ctx the answer is exactly Can(ctx, capability) — the
// fail-closed semantics of the coarse role policy are unchanged, so existing
// RBAC-only wiring is byte-identical. A nil context fails closed to false.
//
// Can itself is untouched: there is no wildcard/segment logic in the hot path.
// The resource-aware path is a separate entrypoint the CRUD layer opts into.
func CanResource(ctx context.Context, capability Permission, resource Ref) bool {
	if ctx == nil {
		return false
	}
	if d, ok := ctx.Value(deciderKey{}).(Decider); ok && d != nil {
		switch d(ctx, GetRoles(ctx), capability, resource) {
		case DecisionAllow:
			return true
		case DecisionDeny:
			return false
		case DecisionAbstain:
			// fall through to the role policy
		}
	}
	return Can(ctx, capability)
}

// DeciderMiddleware installs a Decider into the request context so downstream
// CanResource calls (auto-CRUD permission gates, RequirePermission-style
// handlers) consult it. Mount it alongside access.Middleware:
//
//	app.Use(access.Middleware(policy, roles.Resolve))
//	app.Use(access.DeciderMiddleware(teamMaintainerDecider))
//
// This shape — a sibling constructor returning the same
// func(http.Handler) http.Handler type — matches Middleware's. The package uses
// positional constructors (Middleware(policy, roles)), not an options pattern,
// so a DeciderMiddleware(d) wrapper is the consistent way to add the decider
// without changing Middleware's signature or introducing an options idiom for
// a single field.
func DeciderMiddleware(d Decider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithDecider(r.Context(), d)))
		})
	}
}
