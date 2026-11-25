package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed sortabledrag.js
var sortabledragJS string

// SortableDragBehaviorName is the runtime module that drags a sortable row by its grip.
const SortableDragBehaviorName = "headless-sortabledrag"

var _ = uiregistry.RegisterBehavior(SortableDragBehaviorName, sortabledragJS,
	uiregistry.Markers("[data-hui-sortable]"),
	uiregistry.Requires(SortableListBehaviorName))
