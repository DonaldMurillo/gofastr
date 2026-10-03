package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed wizard.js
var wizardJS string

// WizardBehaviorName is the runtime module that binds the step
// wizard's data-hui-* hooks. No old standalone module existed: the
// plain POST wizard needed none, and this one owns only the island
// path's focus restore and announcement.
const WizardBehaviorName = "headless-wizard"

// The marker: the wizard's form root.
var _ = uiregistry.RegisterBehavior(WizardBehaviorName, wizardJS,
	uiregistry.Markers("[data-hui-step-wizard]"))
