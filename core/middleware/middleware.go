package middleware

import "slices"

import "net/http"

// Middleware wraps an http.Handler.
type Middleware func(next http.Handler) http.Handler

// Chain composes middleware so Chain(A, B, C)(h) is A(B(C(h))); with none it is a no-op.
func Chain(mw ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for _, m := range slices.Backward(mw) {
			final = m(final)
		}
		return final
	}
}

// Pipeline is an ordered list of middleware; Build wraps them around a final handler.
type Pipeline struct {
	middleware []Middleware
}

// NewPipeline creates a Pipeline with the given middleware.
func NewPipeline(mw ...Middleware) *Pipeline {
	return &Pipeline{middleware: append([]Middleware{}, mw...)}
}

// Use appends middleware to the pipeline.
func (p *Pipeline) Use(mw ...Middleware) *Pipeline {
	p.middleware = append(p.middleware, mw...)
	return p
}

// Build returns Chain(p.middleware...)(final).
func (p *Pipeline) Build(final http.Handler) http.Handler {
	return Chain(p.middleware...)(final)
}
