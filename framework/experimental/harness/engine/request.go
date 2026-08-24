package engine

import (
	"context"
	"slices"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// RequestHandler is the leaf in the request middleware chain, it
// invokes the provider's Chat method and returns the streaming
// channel.
type RequestHandler func(ctx context.Context, req *provider.Request) (<-chan provider.StreamEvent, error)

// RequestMiddleware wraps a RequestHandler. Used for AGENTS.md
// injection, skill activation, memory selection, history compaction,
// cost-budget enforcement, provider routing, etc.
type RequestMiddleware func(ctx context.Context, req *provider.Request, next RequestHandler) (<-chan provider.StreamEvent, error)

// ChainRequest composes a RequestHandler from a base handler and a
// sequence of middleware. First middleware listed is outermost.
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
