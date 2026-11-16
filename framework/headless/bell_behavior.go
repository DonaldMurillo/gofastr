package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed bell.js
var bellJS string

// BellBehaviorName is the runtime module that keeps the bell's accessible name
// saying the count its badge shows.
const BellBehaviorName = "headless-bell"

var _ = uiregistry.RegisterBehavior(BellBehaviorName, bellJS,
	uiregistry.Markers("[data-hui-notification-bell]"))
