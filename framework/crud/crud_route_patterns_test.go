package crud

import (
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// CrudRoutePatterns is a second copy of the route list RegisterCrudRoutes
// mounts, and App.TryEntity pre-flights endpoint collisions against it
// before anything is registered. A route added to the registration and not
// to the pattern list silently reopens that gap, an endpoint could shadow
// the new route, pass validation, and panic mid-commit with the entity
// already in the registry. This pins the two together.
func TestCrudRoutePatternsMatchRegistration(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts CrudRouteOptions
	}{
		{"default", CrudRouteOptions{}},
		{"no llm.md", CrudRouteOptions{NoLLMMD: true}},
		{"read only", CrudRouteOptions{ReadOnly: true}},
		{"read only, no llm.md", CrudRouteOptions{ReadOnly: true, NoLLMMD: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ent := entity.Define("posts", entity.EntityConfig{
				Name: "posts", Table: "posts",
				Fields: []schema.Field{{Name: "title", Type: schema.String}},
			})
			r := router.New()
			RegisterCrudRoutes(r, NewCrudHandler(ent, nil), "/posts", tc.opts)

			var registered []string
			for _, rt := range r.Routes() {
				registered = append(registered, strings.ToUpper(rt.Method)+" "+rt.Pattern)
			}
			predicted := CrudRoutePatterns("/posts", tc.opts)

			sort.Strings(registered)
			sort.Strings(predicted)

			if strings.Join(registered, "\n") != strings.Join(predicted, "\n") {
				t.Errorf("CrudRoutePatterns has drifted from RegisterCrudRoutes\nregistered:\n  %s\npredicted:\n  %s",
					strings.Join(registered, "\n  "), strings.Join(predicted, "\n  "))
			}
		})
	}
}
