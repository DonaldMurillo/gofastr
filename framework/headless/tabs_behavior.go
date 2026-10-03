package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed tabs.js
var tabsJS string

// TabsBehaviorName is the runtime module that binds the tab strips'
// data-hui-* hooks and owns their keyboard contract. It replaces the
// retired core-ui/runtime tabs module (loaded there only through the
// prefetch bridge; here it is a registered behaviour on the strip's
// own marker).
const TabsBehaviorName = "headless-tabs"

// The marker: the strip's root.
var _ = uiregistry.RegisterBehavior(TabsBehaviorName, tabsJS,
	uiregistry.Markers("[data-hui-tabs]"))
