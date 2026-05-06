package router

import (
	"net/http"
	"strings"
)

// Middleware is a function that wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Router wraps http.ServeMux with method-based routing, path parameter
// extraction, middleware chaining, and route grouping.
//
// It uses the Go 1.22+ ServeMux pattern syntax (e.g. "GET /users/{id}")
// which natively supports method matching and path parameter capture.
type Router struct {
	mux          *http.ServeMux
	prefix       string
	middlewares  []Middleware
	notFound     http.Handler
	notAllowed   http.Handler
	parent       *Router
}

// New creates a new Router.
func New() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// Handle registers a handler for the given method and pattern.
// The pattern uses Go 1.22+ ServeMux syntax, e.g. "GET /users/{id}".
func (r *Router) Handle(method, pattern string, handler http.Handler) {
	fullPattern := method + " " + r.prefix + pattern
	r.mux.Handle(fullPattern, r.wrap(handler))
}

// Get registers a handler for GET requests on the given pattern.
func (r *Router) Get(pattern string, handler http.Handler) {
	r.Handle(http.MethodGet, pattern, handler)
}

// Post registers a handler for POST requests on the given pattern.
func (r *Router) Post(pattern string, handler http.Handler) {
	r.Handle(http.MethodPost, pattern, handler)
}

// Put registers a handler for PUT requests on the given pattern.
func (r *Router) Put(pattern string, handler http.Handler) {
	r.Handle(http.MethodPut, pattern, handler)
}

// Delete registers a handler for DELETE requests on the given pattern.
func (r *Router) Delete(pattern string, handler http.Handler) {
	r.Handle(http.MethodDelete, pattern, handler)
}

// Patch registers a handler for PATCH requests on the given pattern.
func (r *Router) Patch(pattern string, handler http.Handler) {
	r.Handle(http.MethodPatch, pattern, handler)
}

// Param extracts a single path parameter by name from the request.
// It uses the Go 1.22+ r.PathValue() method.
func Param(r *http.Request, name string) string {
	return r.PathValue(name)
}

// Params extracts all path parameters from the request.
// It scans the registered pattern for {name} placeholders and extracts
// each value using r.PathValue().
func Params(r *http.Request) map[string]string {
	pattern := r.Pattern
	if pattern == "" {
		return nil
	}
	params := make(map[string]string)
	// Extract param names from the pattern, e.g. "/users/{id}/posts/{postId}"
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '{' {
			end := strings.IndexByte(pattern[i:], '}')
			if end == -1 {
				break
			}
			name := pattern[i+1 : i+end]
			val := r.PathValue(name)
			if val != "" {
				params[name] = val
			}
			i += end
		}
	}
	return params
}

// Use adds middleware to the router. Middleware is applied in the order
// they are added: the first middleware is the outermost wrapper.
func (r *Router) Use(mw ...Middleware) {
	r.middlewares = append(r.middlewares, mw...)
}

// Group creates a sub-router with the given path prefix and optional middleware.
// The sub-router inherits all parent middleware and adds its own on top.
func (r *Router) Group(prefix string, mw ...Middleware) *Router {
	combined := make([]Middleware, 0, len(r.middlewares)+len(mw))
	combined = append(combined, r.middlewares...)
	combined = append(combined, mw...)

	return &Router{
		mux:         r.mux,
		prefix:      r.prefix + prefix,
		middlewares: combined,
		notFound:    r.notFound,
		notAllowed:  r.notAllowed,
		parent:      r,
	}
}

// NotFound sets a custom handler for 404 (Not Found) responses.
func (r *Router) NotFound(handler http.Handler) {
	r.notFound = handler
}

// MethodNotAllowed sets a custom handler for 405 (Method Not Allowed) responses.
func (r *Router) MethodNotAllowed(handler http.Handler) {
	r.notAllowed = handler
}

// ServeHTTP implements http.Handler. It dispatches requests through the
// underlying ServeMux. If no route matches and a custom notFound handler
// is set, it delegates to that handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Check if any route actually matches before delegating to mux.
	// Go 1.22 ServeMux doesn't let us override its built-in 404.
	_, pattern := r.mux.Handler(req)

	if pattern == "" {
		// No route matched
		if r.notFound != nil {
			r.notFound.ServeHTTP(w, req)
			return
		}
		r.mux.ServeHTTP(w, req)
		return
	}

	r.mux.ServeHTTP(w, req)
}

// wrap applies the router's middleware chain to the given handler.
// Middleware is applied in order: the first in the list wraps the outside,
// so it executes first on the way in and last on the way out.
func (r *Router) wrap(handler http.Handler) http.Handler {
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		handler = r.middlewares[i](handler)
	}
	return handler
}
