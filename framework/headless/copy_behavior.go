package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed copy.js
var copyJS string

// CopyBehaviorName is the runtime module that binds the copy control's
// data-hui-copy* hooks: the delegated click that writes the target's
// text to the clipboard and, once the write resolves, says the copied
// label, the status sentence and the optional toast (or the failure
// sentence when the clipboard refuses). Split from headless-feedback,
// whose toast runtime a copy that toasts still rides.
const CopyBehaviorName = "headless-copy"

// The marker: the copy wrapper (ui.CopyButton, CodeBlock's copy, a
// menu's copy row).
var _ = uiregistry.RegisterBehavior(CopyBehaviorName, copyJS,
	uiregistry.Markers("[data-hui-copy]"))
