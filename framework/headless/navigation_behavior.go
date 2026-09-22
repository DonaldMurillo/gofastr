package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed navigation.js
var navigationJS string

// NavigationBehaviorName is the runtime module that binds the
// page-level controls' data-hui-* hooks: the back-to-top link and the
// theme control group. It replaces the retired core-ui/runtime
// backtotop and themeswitch modules.
const NavigationBehaviorName = "headless-navigation"

// The markers: the back-to-top link and the theme group.
var _ = uiregistry.RegisterBehavior(NavigationBehaviorName, navigationJS,
	uiregistry.Markers("[data-hui-back-to-top]", "[data-hui-theme-toggle]"))
