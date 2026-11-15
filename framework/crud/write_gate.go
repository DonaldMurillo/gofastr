package crud

import "context"

// CanUpdateRecordScoped reports whether PUT /<entity>/{id} would let ctx
// through its gates for record id. The in-process UpdateOne asks no RBAC
// question, so a surface writing through it checks each record here first.
func (ch *CrudHandler) CanUpdateRecordScoped(ctx context.Context, id string) bool {
	return ch.canScopedRecord(ctx, opUpdate, id)
}

// CanCreateScoped is CanUpdateRecordScoped for POST /<entity>.
func (ch *CrudHandler) CanCreateScoped(ctx context.Context) bool {
	return ch.canScopedRecord(ctx, opCreate, "")
}

// CanDeleteRecordScoped is CanUpdateRecordScoped for DELETE /<entity>/{id}.
func (ch *CrudHandler) CanDeleteRecordScoped(ctx context.Context, id string) bool {
	return ch.canScopedRecord(ctx, opDelete, id)
}

func (ch *CrudHandler) canScopedRecord(ctx context.Context, op crudOp, id string) bool {
	return ch != nil && ch.Entity != nil && ch.scopeGate(ctx, op, id) == nil
}
