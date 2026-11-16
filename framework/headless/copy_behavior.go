package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed copy.js
var copyJS string

// CopyBehaviorName is the runtime module that binds the copy control's
// data-hui-copy* hooks.
const CopyBehaviorName = "headless-copy"

var _ = uiregistry.RegisterBehavior(CopyBehaviorName, copyJS,
	uiregistry.Markers("[data-hui-copy]"))
