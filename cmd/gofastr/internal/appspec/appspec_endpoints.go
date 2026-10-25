package appspec

import (
	"errors"
	"fmt"
)

// ValidateAppSpecEndpoints checks each endpoint's shape the way the
// settings decoder does (an app spec built in Go never passed through it),
// then what the decoder cannot: the entity is declared, and no two
// endpoints link one handler, since generate app writes one stub per
// handler.
func ValidateAppSpecEndpoints(bp AppSpec) error {
	entities := map[string]bool{}
	for _, d := range bp.Entities {
		entities[d.Name] = true
	}
	names := map[string]bool{}
	handlers := map[string]string{}
	var errs []error
	for _, e := range bp.Endpoints {
		if err := e.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("app spec: endpoint %q: %w", e.Name, err))
			continue
		}
		if names[e.Name] {
			errs = append(errs, fmt.Errorf("app spec: two endpoints are named %s", e.Name))
			continue
		}
		names[e.Name] = true
		if !entities[e.Entity] {
			errs = append(errs, fmt.Errorf("app spec: endpoint %s hangs under entity %q, which the app spec does not declare", e.Name, e.Entity))
		}
		if prev, ok := handlers[e.Handler]; ok {
			errs = append(errs, fmt.Errorf("app spec: endpoints %s and %s both link %s: give each its own handler", prev, e.Name, e.Handler))
			continue
		}
		handlers[e.Handler] = e.Name
	}
	return errors.Join(errs...)
}
