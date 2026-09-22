package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed when.js
var whenJS string

// WhenBehaviorName is the runtime module that binds ConditionalField's
// data-hui-when regions, split from the headless module to keep both
// under the byte budget. The when hooks it owns moved with it.
const WhenBehaviorName = "headless-when"

// The marker: the region root. The watched field is resolved from the
// region's own data-hui-when attributes at every event.
var _ = uiregistry.RegisterBehavior(WhenBehaviorName, whenJS,
	uiregistry.Markers("[data-hui-when]"))
