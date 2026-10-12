package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// elevationKey marks a context whose caller passed a back office's own
// gate. Unexported, so no request-derived context can carry it.
type elevationKey struct{}

// WithElevation returns a context that satisfies the EntityConfig.Access
// permissions (Exposure.Access Read, Create, Update and Delete) of the
// named entities without the caller's roles holding them. It is how
// battery/admin lets an admin manage the entities it exposes, whose
// permissions the admin role was never granted, one call at a time,
// without putting the Wildcard on the context. An entity it does not
// name keeps its Access check: a relation picker, a hook or a stat that
// reaches past the back office's own entities reads as the caller. A
// call naming no entity lifts nothing, and a later call replaces the
// set rather than adding to it.
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
func WithElevation(ctx context.Context, entities ...string) context.Context {
	set := make(map[string]bool, len(entities))
	for _, e := range entities {
		set[e] = true
	}
	return context.WithValue(ctx, elevationKey{}, set)
}

// WithoutElevation returns ctx with WithElevation's lift removed, for code
// a back office runs on the caller's behalf but does not vouch for: an
// app's actions, tabs, view funcs and field kinds run as the caller, so
// entityui hands them this context.
func WithoutElevation(ctx context.Context) context.Context {
	if set, _ := ctx.Value(elevationKey{}).(map[string]bool); len(set) == 0 {
		return ctx
	}
	return context.WithValue(ctx, elevationKey{}, map[string]bool(nil))
}

// runHooks runs reg's typ chain as the caller. A lifecycle hook is app
// code a back office does not vouch for, so WithElevation's lift is
// removed from ctx and from the request a read payload carries: a hook's
// own reads and writes pass only the caller's gates.
func runHooks(reg *hook.HookRegistry, ctx context.Context, typ hook.HookType, data any) error {
	switch p := data.(type) {
	case *hook.ListPayload:
		p.Request = callerRequest(p.Request)
	case *hook.GetPayload:
		p.Request = callerRequest(p.Request)
	}
	return reg.ExecuteHooks(WithoutElevation(ctx), typ, data)
}

// callerRequest is r with WithElevation's lift removed from its context.
func callerRequest(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	if ctx := WithoutElevation(r.Context()); ctx != r.Context() {
		return r.WithContext(ctx)
	}
	return r
}

// elevated reports whether ctx carries WithElevation naming entity.
func elevated(ctx context.Context, entity string) bool {
	set, _ := ctx.Value(elevationKey{}).(map[string]bool)
	return set[entity]
}

// accessAllows answers one EntityConfig.Access check: may ctx exercise
// perm on this entity (id "" for a collection-level op). Every
// Exposure.Access check goes through it, so WithElevation lifts all of
// them alike.
func (ch *CrudHandler) accessAllows(ctx context.Context, perm, id string) bool {
	ref := access.Ref{Type: ch.Entity.GetName(), ID: id}
	if elevated(ctx, ref.Type) {
		d := access.GetDecider(ctx)
		return d == nil || d(ctx, access.GetRoles(ctx), access.Permission(perm), ref) != access.DecisionDeny
	}
	return access.CanResource(ctx, access.Permission(perm), ref)
}
