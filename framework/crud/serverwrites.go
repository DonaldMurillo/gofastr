package crud

import "context"

type serverWritesKey struct{}

// WithServerWrites returns a context marked as trusted server-origin:
// CreateOne, UpdateOne, UpsertOne and the batch methods then persist
// ReadOnly and Hidden fields the body carries instead of skipping them.
// It runs no role check, so set it only from server-side Go, never from
// request data. Owner and tenant columns stay context-derived, and Hidden
// fields stay out of the response.
func WithServerWrites(ctx context.Context) context.Context {
	return context.WithValue(ctx, serverWritesKey{}, true)
}

func serverWrites(ctx context.Context) bool {
	v, _ := ctx.Value(serverWritesKey{}).(bool)
	return v
}
