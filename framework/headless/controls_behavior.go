package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed controls.js
var controlsJS string

// ControlsBehaviorName is the runtime module that binds the counter,
// number input, slider and range slider hooks.
const ControlsBehaviorName = "headless-controls"

var _ = uiregistry.RegisterBehavior(ControlsBehaviorName, controlsJS,
	uiregistry.Markers("[data-hui-counter-value]", "[data-hui-counter-animate]", "[data-hui-number-input-decrement]",
		"[data-hui-slider-output]", "[data-hui-range-slider]", "[data-hui-json]"))
