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
		{"states", CrudRouteOptions{States: statesInvoiceStates()}},
		{"states, read only", CrudRouteOptions{ReadOnly: true, States: statesInvoiceStates()}},
		{"states, no llm.md", CrudRouteOptions{NoLLMMD: true, States: statesInvoiceStates()}},
		{"all-system states", CrudRouteOptions{States: &entity.StatesConfig{
			Field:   "status",
			Initial: []string{"draft"},
			Transitions: []entity.Transition{
				{Key: "mark_overdue", From: []string{"draft"}, To: "void", System: true},
			},
		}}},
		{"composite key", CrudRouteOptions{Key: []string{"scope", "name"}}},
		{"composite key, states", CrudRouteOptions{Key: []string{"scope", "name"}, States: statesInvoiceStates()}},
		{"composite key named key", CrudRouteOptions{Key: []string{"scope", "key"}, States: statesInvoiceStates()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := []schema.Field{{Name: "title", Type: schema.String}}
			for _, c := range tc.opts.Key {
				fields = append(fields, schema.Field{Name: c, Type: schema.String, Required: true})
			}
			ent := entity.Define("posts", entity.EntityConfig{
				Name: "posts", Table: "posts",
				Fields:     fields,
				States:     tc.opts.States,
				PrimaryKey: tc.opts.Key,
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
