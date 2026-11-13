package appgen

import (
	"context"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Each view_stubs_<entity>_init.go file appends caller-dependent views at init.
var generatedViewStubs [][2]string

func withViewStubs(fwApp *framework.App, ext entityui.Extensions) entityui.Extensions {
	return fillViewStubs(fwApp.Registry, ext, generatedViewStubs)
}

// fillViewStubs is emitted verbatim in view_stubs.go. Keeping it
// compiled here lets the runtime test exercise exactly what apps receive.
func fillViewStubs(reg entity.Registry, ext entityui.Extensions, views [][2]string) entityui.Extensions {
	ext.Entities = maps.Clone(ext.Entities)
	if ext.Entities == nil {
		ext.Entities = map[string]entityui.Extension{}
	}
	for _, view := range views {
		// Get, as entityui resolves extension keys: it refuses an
		// extension for a name registered only under versions as an
		// unknown entity, so a stub there could not make boot pass.
		declared, err := reg.Get(view[0])
		if err != nil || declared.Config.Display == nil {
			continue
		}
		needsFilter := false
		for _, current := range declared.Config.Display.Views {
			if current.Key == view[1] && current.Where == "" {
				needsFilter = true
				break
			}
		}
		if !needsFilter {
			continue
		}
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
