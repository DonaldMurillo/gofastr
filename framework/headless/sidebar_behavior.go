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
	uiregistry.Markers("[data-hui-sidebar]"),
	// The mobile drawer's trap and return-focus are the widget
	// runtime's; this module never reimplements them.
	uiregistry.Requires("widgets"))
