package access

import "context"

type heldScopesKey struct{}

type heldScopes struct{ grants []Permission }

// WithHeldScopes marks ctx as scope-restricted to scopes (resource:verb), as an
// API token or embed grant is. An empty or nil slice is maximally restricted.
func WithHeldScopes(ctx context.Context, scopes []string) context.Context {
	grants := make([]Permission, len(scopes))
	for i, s := range scopes {
		grants[i] = Permission(s)
	}
	return context.WithValue(ctx, heldScopesKey{}, heldScopes{grants: grants})
}

// HeldScopes returns ctx's scopes and true, or (nil, false) when ctx is not
// scope-restricted (a session, a JWT, server code).
func HeldScopes(ctx context.Context) ([]Permission, bool) {
	if ctx == nil {
		return nil, false
	}
	h, ok := ctx.Value(heldScopesKey{}).(heldScopes)
	if !ok {
		return nil, false
	}
	return h.grants, true
}

// RoleScope is the scope a scope-restricted caller needs to pass a gate on role:
// "role:<role>". "role:*" and "*:*" cover every role.
func RoleScope(role string) Permission { return Permission("role:" + role) }

// ScopeAllows reports whether ctx may exercise required: always for an unscoped
// ctx, otherwise when ScopeMatch finds a granting scope.
func ScopeAllows(ctx context.Context, required Permission) bool {
	held, scoped := HeldScopes(ctx)
	if !scoped {
		return true
	}
	return ScopeMatch(held, required)
}
