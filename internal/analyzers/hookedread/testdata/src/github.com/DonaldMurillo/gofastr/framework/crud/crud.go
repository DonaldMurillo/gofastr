// Package crud is a stub of the read API the rule knows.
package crud

import "context"

type CrudHandler struct{}

type ListOptions struct{ Limit int }

type GroupCount struct{}

func WithReadHooks(ctx context.Context) context.Context { return ctx }

func (ch *CrudHandler) GetOne(ctx context.Context, id string, includes []string) (map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) ListAll(ctx context.Context, opts ListOptions) ([]map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) CountAll(ctx context.Context, opts ListOptions) (int, error) { return 0, nil }

func (ch *CrudHandler) SumAll(ctx context.Context, field string, opts ListOptions) (string, error) {
	return "", nil
}

func (ch *CrudHandler) GroupCountAll(ctx context.Context, field string, opts ListOptions, limit int) ([]GroupCount, error) {
	return nil, nil
}

func WithStateOverride(ctx context.Context, reason string) context.Context { return ctx }

func (ch *CrudHandler) CreateOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) UpdateOne(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) DeleteOne(ctx context.Context, id string) error  { return nil }
func (ch *CrudHandler) RestoreOne(ctx context.Context, id string) error { return nil }
func (ch *CrudHandler) PurgeOne(ctx context.Context, id string) error   { return nil }

func (ch *CrudHandler) UpsertOne(ctx context.Context, body map[string]any) (map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) RunTransition(ctx context.Context, id, key string) (map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) BatchCreateMany(ctx context.Context, bodies []map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) BatchUpdateMany(ctx context.Context, ids []string, bodies []map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (ch *CrudHandler) BatchDeleteMany(ctx context.Context, ids []string) ([]string, error) {
	return nil, nil
}

// CanUpdateRecordScoped is a gate, neither a read nor a write the rule
// holds.
func (ch *CrudHandler) CanUpdateRecordScoped(ctx context.Context, id string) bool { return true }

type TypedQuery[T any] struct{}

func NewTypedQuery[T any](h *CrudHandler) *TypedQuery[T] { return &TypedQuery[T]{} }

func (q *TypedQuery[T]) Find(ctx context.Context) ([]*T, error)   { return nil, nil }
func (q *TypedQuery[T]) First(ctx context.Context) (*T, error)    { return nil, nil }
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error)   { return 0, nil }
func (q *TypedQuery[T]) Exists(ctx context.Context) (bool, error) { return false, nil }

func (q *TypedQuery[T]) UpdateAll(ctx context.Context, fields map[string]any) (int, error) {
	return 0, nil
}

func (q *TypedQuery[T]) DeleteAll(ctx context.Context) (int, error) { return 0, nil }
