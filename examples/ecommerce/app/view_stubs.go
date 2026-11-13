//gofastr:generated generator=app kind=scaffold version=dev sum=037b662857fd590e

package main

import (
	"context"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Per-entity derived files append their caller-dependent views at init.
var generatedViewStubs [][2]string

func withViewStubs(ext entityui.Extensions) entityui.Extensions {
	return fillViewStubs(ext, generatedViewStubs)
}

// fillViewStubs is emitted verbatim in view_stubs.go. Keeping it
// compiled here lets the runtime test exercise exactly what apps receive.
func fillViewStubs(ext entityui.Extensions, views [][2]string) entityui.Extensions {
	ext.Entities = maps.Clone(ext.Entities)
	if ext.Entities == nil {
		ext.Entities = map[string]entityui.Extension{}
	}
	for _, view := range views {
		e := ext.Entities[view[0]]
		vf := e.Views[view[1]]
		if vf.Filter != nil {
			continue
		}
		e.Views = maps.Clone(e.Views)
		if e.Views == nil {
			e.Views = map[string]entityui.ViewFunc{}
		}
		vf.Filter = func(context.Context) (*filter.Predicate, error) {
			return nil, fmt.Errorf("entity %q view %q: filter is not written yet; implement it in extensions.go", view[0], view[1])
		}
		e.Views[view[1]] = vf
		ext.Entities[view[0]] = e
	}
	return ext
}
