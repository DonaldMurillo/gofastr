package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed sidebar.js
var sidebarJS string

// SidebarBehaviorName is the runtime module that binds the sidebar's
// data-hui-* hooks: the collapse state (persisting only when a storage
// key rides the root — server-owned otherwise) and the toggle. The
// mobile drawer is the widget runtime's. It replaces the retired
// core-ui/runtime sidebar module.
const SidebarBehaviorName = "headless-sidebar"

var _ = uiregistry.RegisterBehavior(SidebarBehaviorName, sidebarJS,
	// The group toggle is a marker of its own: SidebarRegion (and
	// ui.SidebarBody on it) renders button-dialect groups with no
	// data-hui-sidebar shell, and its groups open only through this
	// module's document-level click handler.
	uiregistry.Markers("[data-hui-sidebar]", "[data-hui-sidebar-group-toggle]"),
	// The mobile drawer's trap and return-focus are the widget
	// runtime's; this module never reimplements them.
	uiregistry.Requires("widgets"))
