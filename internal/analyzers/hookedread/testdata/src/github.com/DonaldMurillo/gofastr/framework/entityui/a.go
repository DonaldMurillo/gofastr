package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/crud"
)

type meta struct{ ch *crud.CrudHandler }

type note struct{}

// The pre-chokepoint shape: a screen picking per call whether the
// hooks run. A bare ctx draws stored values and rows a hook hides.
func recordScreen(ctx context.Context, m *meta, id string) {
	_, _ = m.ch.GetOne(ctx, id, nil)                           // want `CrudHandler.GetOne reads stored values`
	_, _ = m.ch.ListAll(ctx, crud.ListOptions{})               // want `CrudHandler.ListAll reads stored values`
	_, _ = m.ch.CountAll(ctx, crud.ListOptions{})              // want `CrudHandler.CountAll reads stored values`
	_, _ = m.ch.SumAll(ctx, "amount", crud.ListOptions{})      // want `CrudHandler.SumAll reads stored values`
	_, _ = m.ch.GroupCountAll(ctx, "s", crud.ListOptions{}, 0) // want `CrudHandler.GroupCountAll reads stored values`
	_, _ = crud.NewTypedQuery[note](m.ch).Find(ctx)            // want `TypedQuery.Find reads stored values`
	_, _ = crud.NewTypedQuery[note](m.ch).First(ctx)           // want `TypedQuery.First reads stored values`
	_, _ = crud.NewTypedQuery[note](m.ch).Count(ctx)           // want `TypedQuery.Count reads stored values`
	_, _ = crud.NewTypedQuery[note](m.ch).Exists(ctx)          // want `TypedQuery.Exists reads stored values`
}

// A local reassigned after the hooked bind is not hooked any more.
func reassigned(ctx context.Context, m *meta, id string) {
	hctx := crud.WithReadHooks(ctx)
	hctx = ctx
	_, _ = m.ch.GetOne(hctx, id, nil) // want `CrudHandler.GetOne reads stored values`
}

// A context derived from a hooked one is a different expression: the
// rule cannot see the opt-in survive, so it asks for the reader.
func derived(ctx context.Context, m *meta, id string) {
	_, _ = m.ch.GetOne(context.WithoutCancel(crud.WithReadHooks(ctx)), id, nil) // want `CrudHandler.GetOne reads stored values`
}

// The fix: the hooked reader.
func (m *meta) getRow(ctx context.Context, id string) (map[string]any, error) {
	return m.ch.GetOne(crud.WithReadHooks(ctx), id, nil)
}

// A local bound once to the opt-in is the hooked read too.
func hookedLocal(ctx context.Context, m *meta) {
	hctx := crud.WithReadHooks(ctx)
	_, _ = m.ch.ListAll(hctx, crud.ListOptions{})
	_, _ = m.ch.CountAll(hctx, crud.ListOptions{})
	_, _ = crud.NewTypedQuery[note](m.ch).Find(hctx)
}

// Silent: a named stored-value reader.
func (m *meta) getRowUnhooked(ctx context.Context, id string) (map[string]any, error) {
	return m.ch.GetOne(ctx, id, nil)
}

// A write on a bare ctx skips the BeforeGet check on its target: a row
// GET answers 404 for is written.
func write(ctx context.Context, m *meta, id string) {
	_, _ = m.ch.CreateOne(ctx, nil)                                                      // want `CrudHandler.CreateOne writes without the read hooks`
	_, _ = m.ch.UpdateOne(ctx, id, nil)                                                  // want `CrudHandler.UpdateOne writes without the read hooks`
	_ = m.ch.DeleteOne(ctx, id)                                                          // want `CrudHandler.DeleteOne writes without the read hooks`
	_ = m.ch.RestoreOne(ctx, id)                                                         // want `CrudHandler.RestoreOne writes without the read hooks`
	_ = m.ch.PurgeOne(ctx, id)                                                           // want `CrudHandler.PurgeOne writes without the read hooks`
	_, _ = m.ch.UpsertOne(ctx, nil)                                                      // want `CrudHandler.UpsertOne writes without the read hooks`
	_, _ = m.ch.RunTransition(ctx, id, "send")                                           // want `CrudHandler.RunTransition writes without the read hooks`
	_, _ = m.ch.BatchCreateMany(ctx, nil)                                                // want `CrudHandler.BatchCreateMany writes without the read hooks`
	_, _ = m.ch.BatchUpdateMany(ctx, nil, nil)                                           // want `CrudHandler.BatchUpdateMany writes without the read hooks`
	_, _ = m.ch.BatchDeleteMany(ctx, nil)                                                // want `CrudHandler.BatchDeleteMany writes without the read hooks`
	_, _ = crud.NewTypedQuery[note](m.ch).UpdateAll(ctx, nil)                            // want `TypedQuery.UpdateAll writes every matching row with no read hooks`
	_, _ = crud.NewTypedQuery[note](m.ch).DeleteAll(ctx)                                 // want `TypedQuery.DeleteAll writes every matching row with no read hooks`
	_, _ = m.ch.UpdateOne(crud.WithStateOverride(crud.WithReadHooks(ctx), "r"), id, nil) // want `CrudHandler.UpdateOne writes without the read hooks`
}

// The fix: the opt-in is the outermost context option, or a local bound
// once to it (a parameter rebound once counts).
func hookedWrite(ctx context.Context, m *meta, id string) {
	_, _ = m.ch.UpdateOne(crud.WithReadHooks(crud.WithStateOverride(ctx, "r")), id, nil)
	ctx = crud.WithReadHooks(ctx)
	_ = m.ch.DeleteOne(ctx, id)
}

// UpdateAll and DeleteAll apply no read hooks under any context, so the
// opt-in does not silence them.
func hooklessWrite(ctx context.Context, m *meta) {
	ctx = crud.WithReadHooks(ctx)
	_, _ = crud.NewTypedQuery[note](m.ch).UpdateAll(crud.WithReadHooks(ctx), nil) // want `TypedQuery.UpdateAll writes every matching row with no read hooks`
	_, _ = crud.NewTypedQuery[note](m.ch).DeleteAll(ctx)                          // want `TypedQuery.DeleteAll writes every matching row with no read hooks`
}

// A write gets no *Unhooked exemption: no screen writes a row its caller
// cannot read.
func (m *meta) saveUnhooked(ctx context.Context, id string) {
	_, _ = m.ch.UpdateOne(ctx, id, nil) // want `CrudHandler.UpdateOne writes without the read hooks`
}

// Near miss: a gate is neither a read nor a write.
func gate(ctx context.Context, m *meta, id string) bool {
	return m.ch.CanUpdateRecordScoped(ctx, id)
}

// Near miss: a method of the same name on another type.
type store struct{}

func (store) GetOne(ctx context.Context, id string, includes []string) (map[string]any, error) {
	return nil, nil
}

func otherType(ctx context.Context, s store) {
	_, _ = s.GetOne(ctx, "x", nil)
}

// Near miss: "Unhooked" must end the name, not appear inside it.
func unhookedButNot(ctx context.Context, m *meta) {
	_, _ = m.ch.ListAll(ctx, crud.ListOptions{}) // want `CrudHandler.ListAll reads stored values`
}
