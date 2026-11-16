package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed panehost.js
var panehostJS string

// PaneHostBehaviorName is the module that binds the pane host's open, close and swap lifecycle, focus, drawer and deep link.
const PaneHostBehaviorName = "headless-panehost"

var _ = uiregistry.RegisterBehavior(PaneHostBehaviorName, panehostJS,
	uiregistry.Markers("[data-hui-panehost]"))
