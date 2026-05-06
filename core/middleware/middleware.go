// Package middleware provides HTTP middleware primitives for GoFastr.
//
// The core type is Middleware: func(next http.Handler) http.Handler.
// Middleware can be composed with Chain and assembled into a Pipeline.
package middleware

import "net/http"

// Middleware is a function that wraps an http.Handler, returning a new one.
// It is the fundamental building block for HTTP middleware composition.
type Middleware func(next http.Handler) http.Handler

// Chain composes zero or more middleware into a single Middleware.
// The resulting middleware applies the input middleware in order:
// Chain(A, B, C)(handler) produces A(B(C(handler))).
// This means the first middleware's pre-processing runs first,
// and its post-processing runs last (A→B→C→handler→C→B→A).
//
// With no arguments, Chain returns a no-op middleware.
func Chain(mw ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			final = mw[i](final)
		}
		return final
	}
}

// Pipeline is an ordered list of middleware with a final handler.
// Use Build to produce a single http.Handler that runs all middleware
// in sequence around the final handler.
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

// Build composes all pipeline middleware around the final handler
// and returns a single http.Handler.
//
// Build(h) is equivalent to Chain(p.middleware...)(h).
func (p *Pipeline) Build(final http.Handler) http.Handler {
	return Chain(p.middleware...)(final)
}
