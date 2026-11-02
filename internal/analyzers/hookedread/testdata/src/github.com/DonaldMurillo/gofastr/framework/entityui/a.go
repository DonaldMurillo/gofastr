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

// Near miss: a write takes a bare ctx and is not a read.
func write(ctx context.Context, m *meta, id string) {
	_, _ = m.ch.UpdateOne(ctx, id, nil)
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
