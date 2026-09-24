package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed multiselect.js
var multiselectJS string

// MultiSelectBehaviorName is the runtime module that binds the
// multiselect's data-hui-* hooks: the chips strip (rebuilt from the
// checkboxes' own state after every change), the chip remove buttons
// and the click-outside close. It replaces the retired core-ui/runtime
// multiselect module.
const MultiSelectBehaviorName = "headless-multiselect"

// The marker: the multiselect root. The disclosure half — Escape to
// close with focus returned, the aria-expanded mirror — is
// headless-disclosure's (the details the component renders carries
// data-hui-disclosure), so this module requires it rather than
// growing a second disclosure implementation.
var _ = uiregistry.RegisterBehavior(MultiSelectBehaviorName, multiselectJS,
	uiregistry.Markers("[data-hui-multiselect]"),
	uiregistry.Requires(DisclosureBehaviorName))
