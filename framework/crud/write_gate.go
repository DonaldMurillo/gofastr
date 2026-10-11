package crud

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// CanUpdateRecordScoped reports whether ctx may update the record id: the
// boolean mirror of the gates PUT /<entity>/{id} runs before the write
// (requireScope with opUpdate), with the Decider asked about this record.
// A surface that writes outside the routes, such as an entityui bulk
// action, checks each record with it before the in-process UpdateOne,
// which applies owner and tenant scope but asks no RBAC question.
func (ch *CrudHandler) CanUpdateRecordScoped(ctx context.Context, id string) bool {
	return ch.canScopedRecord(ctx, opUpdate, id)
}

// CanCreateScoped is CanUpdateRecordScoped for POST /<entity>: the gates
// a create runs before the write, with the Decider asked about the
// collection (Ref.ID ""). A screen asks it before drawing New or a
// create form.
func (ch *CrudHandler) CanCreateScoped(ctx context.Context) bool {
	return ch.canScopedRecord(ctx, opCreate, "")
}

// CanDeleteRecordScoped is CanUpdateRecordScoped for DELETE /<entity>/{id}.
func (ch *CrudHandler) CanDeleteRecordScoped(ctx context.Context, id string) bool {
	return ch.canScopedRecord(ctx, opDelete, id)
}

// canScopedRecord is requireScope(op) as a boolean about one record: the
// default posture's session requirement, owner, tenant, then op's
// permission asked about Ref{Type, ID}. Deliberately not
// requireOwnerContext: that honours owner.AllowCrossOwner and the route's
// RequireOwner does not, so a host applying the exported marker to a
// request context would make screens show or write rows the route
// refuses. It answers the stricter of the two.
func (ch *CrudHandler) canScopedRecord(ctx context.Context, op crudOp, id string) bool {
	if ch == nil || ch.Entity == nil {
		return false
	}
	if ch.sessionGated() {
		if _, ok := handler.GetUser(ctx); !ok {
			return false
		}
	}
	if ch.Entity.Config.Scope.OwnerField != "" {
		if _, ok := owner.Get(ctx); !ok {
			return false
		}
	}
	if ch.requireTenantContext(ctx) != nil {
		return false
	}
	perm := ch.permissionForOp(op)
	if perm == "" {
		return true
	}
	return ch.accessAllows(ctx, perm, id)
}
