package framework

import "github.com/DonaldMurillo/gofastr/framework/routegroup"

// Re-export the routegroup package types so external callers can use
// framework.RouteGroup, routegroup.WithMiddleware, etc. without a
// direct import.

// RouteGroup is the App-level route group abstraction. Created via App.Group().
type RouteGroup = routegroup.RouteGroup

// GroupOption is re-exported for routegroup configuration.
type GroupOption = routegroup.GroupOption
