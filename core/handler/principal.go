package handler

import "context"

// PrincipalCheck returns the context a fresh request with the same
// credentials would carry, or false when it would be refused or anonymous.
type PrincipalCheck func(ctx context.Context) (context.Context, bool)

type principalCheckKey struct{}

type principalChecks struct {
	identity PrincipalCheck
	steps    []PrincipalCheck
}

// WithPrincipalCheck installs check as the identity re-validation; nil
// installs none. A long-lived handler calls RecheckPrincipal per delivery.
func WithPrincipalCheck(ctx context.Context, check PrincipalCheck) context.Context {
	if check == nil {
		return context.WithValue(ctx, principalCheckKey{}, (*principalChecks)(nil))
	}
	return context.WithValue(ctx, principalCheckKey{}, &principalChecks{identity: check})
}

// AddPrincipalCheck appends a step that runs after the identity check on
// the context it returned.
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

// RecheckPrincipal runs the installed checks and returns the refreshed
// context, still carrying them, or (ctx, false) when one refuses. With
// none installed it returns (ctx, true).
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
