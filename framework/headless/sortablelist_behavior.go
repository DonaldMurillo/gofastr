package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed sortablelist.js
var sortablelistJS string

// SortableListBehaviorName is the runtime module that binds the sortable list's data-hui-* hooks.
const SortableListBehaviorName = "headless-sortablelist"

var _ = uiregistry.RegisterBehavior(SortableListBehaviorName, sortablelistJS,
	uiregistry.Markers("[data-hui-sortable]"))
