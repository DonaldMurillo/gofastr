package router

import "context"

// ResourceGroup returns a Group under r whose routes carry resource in their request context.
func (r *Router) ResourceGroup(resource string) *Router {
	g := r.Group("")
	g.resource = resource
	return g
}

type routeResourceKey struct{}

// RouteResource returns the ResourceGroup name of the matched route; ok is false outside one.
func RouteResource(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(routeResourceKey{}).(string)
	return s, ok && s != ""
}
