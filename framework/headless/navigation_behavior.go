package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed navigation.js
var navigationJS string

// NavigationBehaviorName is the module that binds back-to-top, the theme controls and the keyboard shortcuts.
const NavigationBehaviorName = "headless-navigation"

var _ = uiregistry.RegisterBehavior(NavigationBehaviorName, navigationJS,
	uiregistry.Markers("[data-hui-back-to-top]", "[data-hui-theme-toggle]", "[data-hui-theme-picker]",
		"[data-hui-shortcut-focus]", "[data-hui-shortcut-click]"))
