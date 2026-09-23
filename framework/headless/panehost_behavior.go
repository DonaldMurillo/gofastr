package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed panehost.js
var panehostJS string

// PaneHostBehaviorName is the runtime module that binds the pane
// host's data-hui-* hooks: the open/close/swap lifecycle, the focus
// handoff and restore, the responsive drawer (its Tab trap over the
// kernel's focus selector and the kernel's refcounted scroll lock),
// the programmatic API and events, and the query deep link. It
// replaces the retired core-ui/runtime panehost module.
const PaneHostBehaviorName = "headless-panehost"

var _ = uiregistry.RegisterBehavior(PaneHostBehaviorName, panehostJS,
	uiregistry.Markers("[data-hui-panehost]"))
