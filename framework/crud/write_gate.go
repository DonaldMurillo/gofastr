package crud

import "context"

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

// canScopedRecord reports whether requireScope(op) would let ctx through
// for record id: it asks scopeGate, the decision the route writes.
func (ch *CrudHandler) canScopedRecord(ctx context.Context, op crudOp, id string) bool {
	return ch != nil && ch.Entity != nil && ch.scopeGate(ctx, op, id) == nil
}
