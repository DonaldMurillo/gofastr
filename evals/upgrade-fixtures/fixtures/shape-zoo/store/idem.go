// Package store implements middleware.IdempotencyStore without importing
// core/middleware: the response type arrives through the kitx alias. The
// Finish note matches the method's shape, read with the alias resolved,
// so only the declaration spelling the old signature is the hit; Begin
// and the parameter line use the response type without changing shape.
package store

import (
	"context"

	"example.com/shape-zoo/kitx"
)

type Store struct{}

func (s *Store) Begin(ctx context.Context, key, fingerprint string) (*kitx.Resp, bool, error) {
	return nil, true, nil
}

// The name line names no kit type: only the interface reaches it.
func (s *Store) Finish(ctx context.Context, key string, // zoo:hit finish
	resp *kitx.Resp) error {
	return nil
}
