package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed menu.js
var menuJS string

// MenuBehaviorName is the module that binds the menu's hooks and its keyboard contract.
const MenuBehaviorName = "headless-menu"

var _ = uiregistry.RegisterBehavior(MenuBehaviorName, menuJS,
	uiregistry.Markers("[data-hui-menu]", "[data-hui-menu-trigger]"),
	uiregistry.Requires(DisclosureBehaviorName, "widgets"))
