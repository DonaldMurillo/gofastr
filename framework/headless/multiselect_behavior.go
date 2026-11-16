package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed multiselect.js
var multiselectJS string

// MultiSelectBehaviorName is the runtime module that binds the
// multiselect's chips strip, chip remove buttons and click-outside close.
const MultiSelectBehaviorName = "headless-multiselect"

var _ = uiregistry.RegisterBehavior(MultiSelectBehaviorName, multiselectJS,
	uiregistry.Markers("[data-hui-multiselect]"),
	uiregistry.Requires(DisclosureBehaviorName))
