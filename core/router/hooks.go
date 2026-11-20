package router

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// SetRegisterHook sets the tree-wide callback fired on every Handle; nil clears it.
func (r *Router) SetRegisterHook(fn func(method, pattern string)) {
	r.root.mu.Lock()
	r.root.registerHook = fn
	r.root.mu.Unlock()
}

// SetRouteGate sets the tree-wide gate keyed "METHOD /pattern"; false or a panic answers 404 and drops the method from Allow.
func (r *Router) SetRouteGate(fn func(pattern string) bool) {
	r.root.mu.Lock()
	r.root.routeGate = fn
	r.root.mu.Unlock()
}

// SetServeHook sets the tree-wide callback fired with each matched route's method and pattern, after the gate and before the chain.
func (r *Router) SetServeHook(fn func(method, pattern string)) {
	r.root.mu.Lock()
	r.root.serveHook = fn
	r.root.mu.Unlock()
}

func serveHookSafely(ctx context.Context, hook func(method, pattern string), method, pattern string) {
	defer textsafe.LogPanic(ctx, "router: serve hook panicked; ignored")
	hook(method, pattern)
}

func routeGateSafely(ctx context.Context, gate func(string) bool, key string) (allowed bool) {
	defer textsafe.LogPanic(ctx, "router: route gate panicked; refused")
	return gate(key)
}

func preRouteSafely(req *http.Request, hook func(*http.Request) (*http.Request, string)) (next *http.Request, target string, completed bool) {
	defer textsafe.LogPanic(req.Context(), "router: pre-route hook panicked; request failed")
	next, target = hook(req)
	return next, target, true
}

// PreRoute sets the tree-wide hook that may rewrite a request before matching, or redirect it with a 308; nil removes it.
func (r *Router) PreRoute(fn func(*http.Request) (*http.Request, string)) {
	if fn == nil {
		r.root.preRoute.Store(nil)
		return
	}
	r.root.preRoute.Store(&fn)
}

func (r *Router) gateAllows(ctx context.Context, key string) bool {
	r.root.mu.RLock()
	gate := r.root.routeGate
	r.root.mu.RUnlock()
	return gate == nil || routeGateSafely(ctx, gate, key)
}
