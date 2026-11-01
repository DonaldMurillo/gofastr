package router

import "context"

// ResourceGroup returns a sub-router with this router's prefix whose
// routes all serve one REST resource, named resource. A request matched
// by one of them carries the name in its context before the middleware
// chain runs; RouteResource reads it back. A middleware that authorizes
// by resource (auth.RequireAPIScopes) then asks the route which resource
// it belongs to instead of guessing from the path, where a table named
// "v1" with a "/_batch" subroute reads like a versioned mount of a table
// named "_batch".
//
// A Group under a ResourceGroup keeps the name; a ResourceGroup under
// another replaces it.
func (r *Router) ResourceGroup(resource string) *Router {
	g := r.Group("")
	g.resource = resource
	return g
}

type routeResourceKey struct{}

// RouteResource returns the resource name the matched route was
// registered under with ResourceGroup. ok is false for a route registered
// outside one, and outside a routed request.
func RouteResource(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(routeResourceKey{}).(string)
	return s, ok && s != ""
}
