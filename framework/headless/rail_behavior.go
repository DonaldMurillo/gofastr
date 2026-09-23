package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed rail.js
var railJS string

// RailBehaviorName is the runtime module that binds the rail's
// data-hui-* hooks and owns the shared IntersectionObserver the table
// of contents arms through. It replaces the retired core-ui/runtime
// scrollspy module.
const RailBehaviorName = "headless-rail"

// The markers: the rail root. A rail with no observation selector is
// still a rail (its links work and nothing is marked), so the marker
// loads the module for both; the module arms only what carries an
// observe selector.
var _ = uiregistry.RegisterBehavior(RailBehaviorName, railJS,
	uiregistry.Markers("[data-hui-rail]"))
