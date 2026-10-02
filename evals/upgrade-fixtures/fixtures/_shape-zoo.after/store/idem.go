// Package store implements middleware.IdempotencyStore without importing
// core/middleware: the response type arrives through the kitx alias.
package store

import (
	"context"

	"example.com/shape-zoo/kitx"
)

type Store struct{}

func (s *Store) Begin(ctx context.Context, key, fingerprint string) (*kitx.Resp, bool, error) {
	return nil, true, nil
}

func (s *Store) Finish(ctx context.Context, key, fingerprint string, resp *kitx.Resp) error {
	return nil
}
