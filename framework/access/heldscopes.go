package access

import "context"

type heldScopesKey struct{}

// heldScopes wraps the granted set so an EMPTY scope list still reads as
// "scoped" (maximally restricted) rather than as "no scopes installed".
type heldScopes struct{ grants []Permission }

// WithHeldScopes marks ctx as carrying scoped authority: the resource:verb
// scopes of an API token or an embed grant. battery/auth's TokenMiddleware
// (through auth.WithTokenScopes) and the embed middleware (through
// embed.WithGrant) install it, so packages that cannot import either, the
// CRUD layer in particular, can see what the request was narrowed to.
//
// A scope-restricted request may reach only the resources its scopes name.
// The route-level gate (auth.RequireAPIScopes) checks the entity in the path;
// the CRUD layer reads this value to hold the same line when one route
// reaches ANOTHER entity: ?include=, ?rel.field= filters and cascade writes.
//
// An empty or nil scopes slice is maximally restricted, never unrestricted.
func WithHeldScopes(ctx context.Context, scopes []string) context.Context {
	grants := make([]Permission, len(scopes))
	for i, s := range scopes {
		grants[i] = Permission(s)
	}
	return context.WithValue(ctx, heldScopesKey{}, heldScopes{grants: grants})
}

// HeldScopes returns the scopes a scope-restricted request holds and true, or
// (nil, false) when ctx carries no scopes: a session, a JWT, or server code,
// none of which the scope model narrows.
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

// ScopeAllows reports whether ctx may exercise required under the scope
// model. An unscoped ctx is allowed: scopes only ever narrow. A scoped ctx is
// allowed when ScopeMatch finds a granting scope.
func ScopeAllows(ctx context.Context, required Permission) bool {
	held, scoped := HeldScopes(ctx)
	if !scoped {
		return true
	}
	return ScopeMatch(held, required)
}
