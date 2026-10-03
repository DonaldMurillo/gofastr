package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed toc.js
var tocJS string

// TOCBehaviorName is the runtime module that binds the table of
// contents' data-hui-* hooks. It replaces the retired core-ui/runtime
// toc module: the list is server-rendered from explicit items now, so
// what the module owns is only the active-entry state.
const TOCBehaviorName = "headless-toc"

// The marker: the contents root. The observer itself is headless-rail's
// (one observer, not two drifting scroll-spies), which the Requires
// declaration loads first.
var _ = uiregistry.RegisterBehavior(TOCBehaviorName, tocJS,
	uiregistry.Markers("[data-hui-toc]"),
	uiregistry.Requires(RailBehaviorName))
