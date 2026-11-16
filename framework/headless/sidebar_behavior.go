package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed sidebar.js
var sidebarJS string

// SidebarBehaviorName is the module that binds the sidebar's collapse state and its collapse and group toggles.
const SidebarBehaviorName = "headless-sidebar"

var _ = uiregistry.RegisterBehavior(SidebarBehaviorName, sidebarJS,
	uiregistry.Markers("[data-hui-sidebar]", "[data-hui-sidebar-group-toggle]"),
	uiregistry.Requires("widgets"))
