package engine

import (
	"context"
	"slices"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// RequestHandler sends a request and returns the provider's stream.
type RequestHandler func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error)

// RequestMiddleware wraps a RequestHandler.
type RequestMiddleware func(ctx context.Context, req *provider.Request, next RequestHandler) (<-chan provider.StreamEvent, error)

// ChainRequest wraps base in ms, the first listed outermost.
func ChainRequest(base RequestHandler, ms ...RequestMiddleware) RequestHandler {
	h := base
	for _, m := range slices.Backward(ms) {

		next := h
		h = func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error) {
			return m(ctx, req, next)
		}
	}
	return h
}
