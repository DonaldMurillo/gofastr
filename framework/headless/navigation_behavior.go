package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed navigation.js
var navigationJS string

// NavigationBehaviorName is the runtime module that binds the
// page-level controls' data-hui-* hooks: the back-to-top link, the
// theme (colour-scheme) control group, and the page's keyboard
// shortcuts. It replaces the retired core-ui/runtime backtotop,
// themeswitch and shortcut modules.
const NavigationBehaviorName = "headless-navigation"

// The markers: the back-to-top link, the theme group, and the two
// shortcut chords (a page with only a focus chord does not pay for a
// scan the theme pass would run).
var _ = uiregistry.RegisterBehavior(NavigationBehaviorName, navigationJS,
	uiregistry.Markers("[data-hui-back-to-top]", "[data-hui-theme-toggle]",
		"[data-hui-shortcut-focus]", "[data-hui-shortcut-click]"))
