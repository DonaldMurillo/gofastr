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

// UpdateOne writes; a write is not a read the rule holds.
func (ch *CrudHandler) UpdateOne(ctx context.Context, id string, body map[string]any) (map[string]any, error) {
	return nil, nil
}

type TypedQuery[T any] struct{}

func NewTypedQuery[T any](h *CrudHandler) *TypedQuery[T] { return &TypedQuery[T]{} }

func (q *TypedQuery[T]) Find(ctx context.Context) ([]*T, error) { return nil, nil }
func (q *TypedQuery[T]) First(ctx context.Context) (*T, error)  { return nil, nil }
func (q *TypedQuery[T]) Count(ctx context.Context) (int, error) { return 0, nil }
