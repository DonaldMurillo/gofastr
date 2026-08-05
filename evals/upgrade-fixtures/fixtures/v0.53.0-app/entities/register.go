package entities

import (
	"sort"

	"github.com/DonaldMurillo/gofastr/framework"
)

// registrar pairs an entity's registration func with its declaration order.
// Each entity file appends one registrar in init(); RegisterAll runs them in
// declaration order, so the package behaves identically regardless of the
// lexical order Go runs each file's init() in. The order field is also how
// gofastr pack recovers the blueprint's authored entity order from source.
type registrar struct {
	order int
	fn    func(app *framework.App)
}

var registrars []registrar

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }

// RegisterAll registers every generated entity declaration with app, in
// declaration order. This file never holds an entity name: add an entity by
// dropping in a new entities/<name>.go that appends to registrars in init().
func RegisterAll(app *framework.App) {
	sort.SliceStable(registrars, func(i, j int) bool {
		return registrars[i].order < registrars[j].order
	})
	for _, r := range registrars {
		r.fn(app)
	}
}
