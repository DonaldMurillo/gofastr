package framework

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// EndpointRouteError identifies the endpoint whose resolved route collided.
// Index is its zero-based position in ValidateEntityEndpointRoutes' endpoints.
type EndpointRouteError struct {
	Index int
	Err   error
}

func (e *EndpointRouteError) Error() string { return e.Err.Error() }
func (e *EndpointRouteError) Unwrap() error { return e.Err }

// ValidateEntityEndpointRoutes checks endpoint collisions before registration.
// Each endpoint's Path must be its resolved, absolute HTTP route, including
// API/group prefixes. mount is the entity's CRUD mount, or empty when CRUD is
// disabled; opts describe that mount. withEntityUI includes its bulk, export
// and picker routes. existing optionally supplies routes already registered
// on the app. No routes or handlers are executed or published.
func ValidateEntityEndpointRoutes(mount string, opts crud.CrudRouteOptions, withEntityUI bool, endpoints []Endpoint, existing ...router.RegisteredRoute) error {
	taken := map[string]string{}
	for _, rt := range existing {
		taken[normalizeRoutePattern(strings.ToUpper(rt.Method)+" "+rt.Pattern)] = "an existing route"
	}
	held := newMuxReplay(existing)
	if mount != "" {
		for _, pattern := range crud.CrudRoutePatterns(mount, opts) {
			taken[normalizeRoutePattern(pattern)] = "this entity's own generated CRUD route"
			if conflict, refused := held.add(pattern, "this entity's own generated CRUD route"); refused {
				return fmt.Errorf("generated CRUD route %s overlaps %s, %s", pattern, conflict.pattern, conflict.owner)
			}
		}
		if withEntityUI {
			for _, pattern := range entityUIRoutePatterns(mount) {
				if conflict, refused := held.add(pattern, "this entity's EntityUI route"); refused {
					return fmt.Errorf("EntityUI route %s overlaps %s, %s", pattern, conflict.pattern, conflict.owner)
				}
				taken[normalizeRoutePattern(pattern)] = "this entity's EntityUI route"
			}
		}
	}
	for i, ep := range endpoints {
		pattern := strings.ToUpper(strings.TrimSpace(ep.Method)) + " " + ep.Path
		if !muxAccepts(http.NewServeMux(), pattern) {
			return &EndpointRouteError{Index: i, Err: fmt.Errorf("endpoint %q has malformed route %q: use a valid absolute ServeMux path and method", ep.Path, pattern)}
		}
		key := normalizeRoutePattern(pattern)
		if owner, clash := taken[key]; clash {
			return &EndpointRouteError{Index: i, Err: fmt.Errorf("endpoint %q would register %s, but that route is %s: rename the endpoint path, or move entity routes under an APIPrefix", ep.Path, key, owner)}
		}
		if conflict, refused := held.add(pattern, "a sibling endpoint on this declaration"); refused {
			return &EndpointRouteError{Index: i, Err: fmt.Errorf("endpoint %q would register %s, which overlaps %s, %s: rename the endpoint path, or move entity routes under an APIPrefix", ep.Path, pattern, conflict.pattern, conflict.owner)}
		}
		taken[key] = "a sibling endpoint on this declaration"
	}
	return nil
}
