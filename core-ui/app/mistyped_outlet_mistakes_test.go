//go:build mistakes

package app_test

// TestMistypedOutletDoesNotCompile's fixture (DESIGN-layout-outlets.md
// "API"): a mistyped outlet handle is a compile error, not a silent
// no-op. Built only under -tags mistakes; the companion test in
// mistyped_outlet_test.go builds this package with the tag and
// asserts the failure names the typo. The string-addressed fills of
// the prototype could not refuse this — app.Addr(shell, "tolbar")
// compiled and silently filled nothing.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var _ = func() {
	// A fill named by a string, the prototype's form: a typo in it
	// compiled and filled nothing. The handle API takes *app.Outlet,
	// so any string address, typo'd or not, is a type error.
	_ = app.NewScreen("/mistake", app.NewStaticComponent(render.Text("x"))).
		Fill("tolbar", app.NewStaticComponent(render.Text("never")))
}
