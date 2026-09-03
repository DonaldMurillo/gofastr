//go:build red

package uihost

// RED TEST companion helpers. This file exists separately from
// uihost_red_test.go because of a go1.27 test-loading quirk in this
// environment: a *_test.go file whose import list contains BOTH
// core-ui/app and core-ui/component intermittently fails module
// resolution ("no required module provides package … core-ui/app").
// Splitting the two imports across files loads cleanly, so every
// component-package symbol the red tests need is routed through here.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// redAction registers a server action whose Go handler runs fn. Used by
// TestUihostRedActionRejectsDuplicateKeys / ...CaseFoldedKeys; the plain
// func() keeps the caller free of a component import.
func redAction(name string, fn func()) {
	component.On(name, func(*component.ComponentContext) { fn() })
}
