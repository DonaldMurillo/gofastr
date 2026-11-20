package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed validity.js
var validityJS string

// ValidityBehaviorName is the runtime module that copies the browser's
// :user-invalid verdict onto aria-invalid for every Field and Choice.
const ValidityBehaviorName = "headless-validity"

var _ = uiregistry.RegisterBehavior(ValidityBehaviorName, validityJS,
	uiregistry.Markers("[data-hui-field]", "[data-hui-choice]"))
