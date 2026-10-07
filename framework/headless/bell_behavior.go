package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed bell.js
var bellJS string

// BellBehaviorName is the runtime module that keeps the notification
// bell's accessible name saying the count its badge shows, when a
// signal changes the badge after first paint.
const BellBehaviorName = "headless-bell"

// The marker: the bell's anchor.
var _ = uiregistry.RegisterBehavior(BellBehaviorName, bellJS,
	uiregistry.Markers("[data-hui-notification-bell]"))
