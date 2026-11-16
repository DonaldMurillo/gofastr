package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed disclosure.js
var disclosureJS string

// DisclosureBehaviorName is the runtime module that binds the disclosure hooks.
const DisclosureBehaviorName = "headless-disclosure"

var _ = uiregistry.RegisterBehavior(DisclosureBehaviorName, disclosureJS,
	uiregistry.Markers("[data-hui-disclosure]"))
