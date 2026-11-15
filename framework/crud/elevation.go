package crud

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type elevationKey struct{}

// WithElevation returns a context that passes the Exposure.Access checks of
// the named entities, and only those, whatever the caller's roles. A later
// call replaces the set. A Decider's deny, owner and tenant scope,
// CrossOwnerRead, transition and state-override permissions, held scopes and
// the session gate all still apply.
//
// SECURITY: set it only from server code that has already decided the caller
// may administer these entities. Never derive it from request data.
func WithElevation(ctx context.Context, entities ...string) context.Context {
	set := make(map[string]bool, len(entities))
	for _, e := range entities {
		set[e] = true
	}
	return context.WithValue(ctx, elevationKey{}, set)
}

// WithoutElevation returns ctx with WithElevation's lift removed, for app code
// a back office runs on the caller's behalf.
func WithoutElevation(ctx context.Context) context.Context {
	if set, _ := ctx.Value(elevationKey{}).(map[string]bool); len(set) == 0 {
		return ctx
	}
	return context.WithValue(ctx, elevationKey{}, map[string]bool(nil))
}

// runHooks runs reg's typ chain without WithElevation, on ctx and on a read
// payload's request.
func runHooks(reg *hook.HookRegistry, ctx context.Context, typ hook.HookType, data any) error {
	switch p := data.(type) {
	case *hook.ListPayload:
		p.Request = callerRequest(p.Request)
	case *hook.GetPayload:
		p.Request = callerRequest(p.Request)
	}
	return reg.ExecuteHooks(WithoutElevation(ctx), typ, data)
}

func callerRequest(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	if ctx := WithoutElevation(r.Context()); ctx != r.Context() {
		return r.WithContext(ctx)
	}
	return r
}

func elevated(ctx context.Context, entity string) bool {
	set, _ := ctx.Value(elevationKey{}).(map[string]bool)
	return set[entity]
}

// accessAllows reports whether ctx may exercise Access permission perm on
// record id ("" for the collection).
func (ch *CrudHandler) accessAllows(ctx context.Context, perm, id string) bool {
	ref := access.Ref{Type: ch.Entity.GetName(), ID: id}
	if elevated(ctx, ref.Type) {
		d := access.GetDecider(ctx)
		return d == nil || d(ctx, access.GetRoles(ctx), access.Permission(perm), ref) != access.DecisionDeny
	}
	if perm == permRefused {
		return false
	}
	return access.CanResource(ctx, access.Permission(perm), ref)
}
