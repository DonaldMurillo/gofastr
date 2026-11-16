package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed when.js
var whenJS string

// WhenBehaviorName is the runtime module that binds ConditionalField's
// data-hui-when regions.
const WhenBehaviorName = "headless-when"

var _ = uiregistry.RegisterBehavior(WhenBehaviorName, whenJS,
	uiregistry.Markers("[data-hui-when]"))
