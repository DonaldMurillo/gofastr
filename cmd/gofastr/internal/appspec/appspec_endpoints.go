package appspec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// ValidateAppSpecEndpoints checks each endpoint's shape and its fit with the rest of the spec.
func ValidateAppSpecEndpoints(bp AppSpec) error {
	entities := map[string]framework.EntityDeclaration{}
	for _, d := range bp.Entities {
		entities[d.Name] = d
	}
	groups := map[string][]framework.Endpoint{}
	indices := map[string][]int{}
	mounts := map[string]string{}
	for _, decl := range bp.Entities {
		mount := "/" + entityTable(decl)
		if p := strings.Trim(bp.RouteApp().APIPrefix, "/"); p != "" {
			mount = "/" + p + mount
		}
		mounts[decl.Name] = mount
	}
	names := map[string]bool{}
	handlers := map[string]string{}
	var errs []error
	for i, e := range bp.Endpoints {
		if err := e.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("app spec: endpoints[%d]: endpoint %q: %w", i, e.Name, err))
			continue
		}
		groups[e.Entity] = append(groups[e.Entity], framework.Endpoint{Method: e.Method, Path: mounts[e.Entity] + "/" + e.Path})
		indices[e.Entity] = append(indices[e.Entity], i)
		if names[e.Name] {
			errs = append(errs, fmt.Errorf("app spec: two endpoints are named %s", e.Name))
			continue
		}
		names[e.Name] = true
		if _, ok := entities[e.Entity]; !ok {
			errs = append(errs, fmt.Errorf("app spec: endpoint %s hangs under entity %q, which the app spec does not declare", e.Name, e.Entity))
		}
		if prev, ok := handlers[e.Handler]; ok {
			errs = append(errs, fmt.Errorf("app spec: endpoints %s and %s both link %s: give each its own handler", prev, e.Name, e.Handler))
			continue
		}
		handlers[e.Handler] = e.Name
	}
	for _, decl := range bp.Entities {
		routes := groups[decl.Name]
		if len(routes) == 0 {
			continue
		}
		cfg, err := decl.Config()
		if err != nil {
			continue
		}
		ownMount := mounts[decl.Name]
		if !EntityDeclarationCRUDEnabled(decl) {
			ownMount = ""
		}
		if err := framework.ValidateEntityEndpointRoutes(ownMount, crud.CrudRouteOptions{NoLLMMD: false, States: cfg.States, Key: cfg.PrimaryKey}, true, routes); err != nil {
			var routeErr *framework.EndpointRouteError
			if errors.As(err, &routeErr) {
				errs = append(errs, fmt.Errorf("app spec: endpoints[%d].path: %w", indices[decl.Name][routeErr.Index], err))
			} else {
				errs = append(errs, fmt.Errorf("app spec: entity %q endpoint routes: %w", decl.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}
