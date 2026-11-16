package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed toc.js
var tocJS string

// TOCBehaviorName is the module that marks the table of contents' active entry.
const TOCBehaviorName = "headless-toc"

var _ = uiregistry.RegisterBehavior(TOCBehaviorName, tocJS,
	uiregistry.Markers("[data-hui-toc]"),
	uiregistry.Requires(RailBehaviorName))
