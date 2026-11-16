package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed wizard.js
var wizardJS string

// WizardBehaviorName is the runtime module that binds the step wizard's data-hui-* hooks on the island path.
const WizardBehaviorName = "headless-wizard"

var _ = uiregistry.RegisterBehavior(WizardBehaviorName, wizardJS,
	uiregistry.Markers("[data-hui-step-wizard]"))
