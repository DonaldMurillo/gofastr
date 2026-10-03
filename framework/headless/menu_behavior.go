package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed menu.js
var menuJS string

// MenuBehaviorName is the runtime module that binds the menu's
// data-hui-* hooks and owns the menu keyboard contract. It replaces
// the retired core-ui/runtime menu module.
const MenuBehaviorName = "headless-menu"

// The markers: the menu root and the caller-owned trigger wrapper. A
// menu IS a disclosure, so headless-disclosure is a requirement (the
// aria mirror, Escape one level at a time, the close-on-navigate live
// there), and the overlay posture coordinates with the widget runtime
// — Escape defers to an open modal, and a menu used as an overlay
// finds the runtime's modal stack and focus utilities loaded.
var _ = uiregistry.RegisterBehavior(MenuBehaviorName, menuJS,
	uiregistry.Markers("[data-hui-menu]", "[data-hui-menu-trigger]"),
	uiregistry.Requires(DisclosureBehaviorName, "widgets"))
