package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed tabs.js
var tabsJS string

// TabsBehaviorName is the module that binds the tab strips' keyboard contract.
const TabsBehaviorName = "headless-tabs"

var _ = uiregistry.RegisterBehavior(TabsBehaviorName, tabsJS,
	uiregistry.Markers("[data-hui-tabs]"))
