package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed behavior.js
var behaviorJS string

// BehaviorName is the runtime module that binds the data-hui-* hooks.
const BehaviorName = "headless"

var behaviorMarkers = []string{
	"[data-hui-reveal]", "[data-hui-color]",
	"[data-hui-form-errors]", "[data-hui-action]", "[data-hui-drop]",
	"[data-hui-system]", "[data-hui-table]", "[data-hui-selection]",
}

var _ = uiregistry.RegisterBehavior(BehaviorName, behaviorJS,
	uiregistry.Markers("[data-hui-reveal]", "[data-hui-color]",
		"[data-hui-form-errors]", "[data-hui-action]", "[data-hui-drop]",
		"[data-hui-system]", "[data-hui-table]", "[data-hui-selection]"),
	uiregistry.Interactions(
		uiregistry.Interaction{
			Event:    "click",
			Selector: "[data-hui-table-sort]",
		},
		uiregistry.Interaction{
			Event:    "click",
			Selector: "[data-hui-page]",
		},
	),
	uiregistry.Requires("action"))
