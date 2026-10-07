package crud

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// elevationKey marks a context whose caller passed a back office's own
// gate. Unexported, so no request-derived context can carry it.
type elevationKey struct{}

// WithElevation returns a context that satisfies every EntityConfig.Access
// permission (Exposure.Access Read, Create, Update and Delete) without
// the caller's roles holding it. It is how battery/admin lets an admin
// manage entities whose permissions the admin role was never granted,
// one call at a time, without putting the Wildcard on the context.
//
// It lifts that one check and nothing else:
//
//   - a Decider's deny still refuses (it is asked first, as in
//     access.CanResource; only its abstain is answered by the lift);
//   - owner and tenant scope still apply, and Scope.CrossOwnerRead is not
//     granted, so an owner-scoped entity still shows only the caller's rows;
//   - a transition's Permission and the state override permission still
//     need the capability by name;
//   - held scopes (API tokens, embed grants) and the session gate still
//     bind, and the caller's roles are left as they are.
//
// SECURITY: set it ONLY from server code that has already decided the
// caller may administer these entities, the way battery/admin does after
// its Authorize gate. NEVER derive it from request data.
func WithElevation(ctx context.Context) context.Context {
	return context.WithValue(ctx, elevationKey{}, true)
}

// WithoutElevation returns ctx with WithElevation's lift removed, for code
// a back office runs on the caller's behalf but does not vouch for: an
// app's actions, tabs, view funcs and field kinds run as the caller, so
// entityui hands them this context.
func WithoutElevation(ctx context.Context) context.Context {
	if !elevated(ctx) {
		return ctx
	}
	return context.WithValue(ctx, elevationKey{}, false)
}

// elevated reports whether ctx carries WithElevation.
func elevated(ctx context.Context) bool {
	v, _ := ctx.Value(elevationKey{}).(bool)
	return v
}

// accessAllows answers one EntityConfig.Access check: may ctx exercise
// perm on this entity (id "" for a collection-level op). Every
// Exposure.Access check goes through it, so WithElevation lifts all of
// them alike.
func (ch *CrudHandler) accessAllows(ctx context.Context, perm, id string) bool {
	ref := access.Ref{Type: ch.Entity.GetName(), ID: id}
	if elevated(ctx) {
		d := access.GetDecider(ctx)
		return d == nil || d(ctx, access.GetRoles(ctx), access.Permission(perm), ref) != access.DecisionDeny
	}
	return access.CanResource(ctx, access.Permission(perm), ref)
}
