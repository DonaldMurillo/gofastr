package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed tooltip.js
var tooltipJS string

// TooltipBehaviorName is the runtime module that raises a Tooltip's pop
// into the top layer and makes it hoverable and dismissable.
const TooltipBehaviorName = "headless-tooltip"

var _ = uiregistry.RegisterBehavior(TooltipBehaviorName, tooltipJS,
	uiregistry.Markers("[data-hui-tooltip]"))
