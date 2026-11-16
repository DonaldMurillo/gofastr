package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed rail.js
var railJS string

// RailBehaviorName is the module that marks the rail's active entry and owns the observer the table of contents shares.
const RailBehaviorName = "headless-rail"

var _ = uiregistry.RegisterBehavior(RailBehaviorName, railJS,
	uiregistry.Markers("[data-hui-rail]"))
