package handler

import "context"

// PrincipalCheck re-validates a long-lived request's principal against live
// state. It receives the context to refresh and returns the context a fresh
// request with the same credentials would carry, or false when that request
// would be refused or would arrive anonymous: the session was revoked, the
// token expired, the owning user was deleted.
type PrincipalCheck func(ctx context.Context) (context.Context, bool)

type principalCheckKey struct{}

// principalChecks is the installed re-validation: the identity step the
// authenticating middleware supplied, then the derivation steps (roles,
// policy) later middleware appended. A nil *principalChecks means "nothing
// to re-check".
type principalChecks struct {
	identity PrincipalCheck
	steps    []PrincipalCheck
}

// WithPrincipalCheck installs check as the request's identity re-validation.
// Identity middleware calls it right after SetUser: the middleware that
// established the principal is the one that knows how to re-establish it.
//
// A request handler reads the principal once, from the context it was handed.
// That is right for a request that answers and ends; it is wrong for one that
// stays open, an SSE stream or a long poll, because the context then outlives
// the authority that built it. Such a handler calls RecheckPrincipal on each
// delivery and closes when it fails.
//
// SetUser clears whatever check was installed, so a check never outlives the
// identity it was built for: a later middleware that switches or clears the
// user must not leave the earlier principal's check behind to resurrect it.
// Pass a nil check to install "nothing to re-check" explicitly.
func WithPrincipalCheck(ctx context.Context, check PrincipalCheck) context.Context {
	if check == nil {
		return context.WithValue(ctx, principalCheckKey{}, (*principalChecks)(nil))
	}
	return context.WithValue(ctx, principalCheckKey{}, &principalChecks{identity: check})
}

// AddPrincipalCheck appends a derivation step that runs after the installed
// identity check, on the context that check returned. Middleware that derives
// request state from the principal, such as access.Middleware resolving roles,
// uses it so the derived state is recomputed for the refreshed principal
// rather than replayed from connect time.
func AddPrincipalCheck(ctx context.Context, step PrincipalCheck) context.Context {
	if step == nil {
		return ctx
	}
	next := &principalChecks{}
	if cur, _ := ctx.Value(principalCheckKey{}).(*principalChecks); cur != nil {
		next.identity = cur.identity
		next.steps = append(next.steps, cur.steps...)
	}
	next.steps = append(next.steps, step)
	return context.WithValue(ctx, principalCheckKey{}, next)
}

// RecheckPrincipal runs the installed re-validation against ctx and returns
// the refreshed context and true, or (ctx, false) when the principal no longer
// holds. With nothing installed it returns (ctx, true): an anonymous request,
// or one authenticated by middleware that installs no check, has nothing to
// re-validate, which is the behaviour every request had before this seam.
//
// The refreshed context keeps the same checks installed, so it can itself be
// re-checked later.
func RecheckPrincipal(ctx context.Context) (context.Context, bool) {
	if ctx == nil {
		return ctx, true
	}
	cur, _ := ctx.Value(principalCheckKey{}).(*principalChecks)
	if cur == nil {
		return ctx, true
	}
	out := ctx
	if cur.identity != nil {
		next, ok := cur.identity(out)
		if !ok || next == nil {
			return ctx, false
		}
		out = next
	}
	for _, step := range cur.steps {
		next, ok := step(out)
		if !ok || next == nil {
			return ctx, false
		}
		out = next
	}
	return context.WithValue(out, principalCheckKey{}, cur), true
}
