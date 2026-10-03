package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed sortablelist.js
var sortablelistJS string

// SortableListBehaviorName is the runtime module that binds the
// sortable list's data-hui-* hooks: the drag-and-drop and keyboard
// reorder, the polite announcements (from Strings that travel as
// attributes), and the commit/rollback/versioned-409 conflict path.
// It replaces the retired core-ui/runtime sortablelist module.
const SortableListBehaviorName = "headless-sortablelist"

// The marker: the list root. The commit is the module's own fetch
// (the CSRF-headered form POST the server contract expects, with the
// same-origin guard), so it requires nothing: the kernel's rpc
// primitive is not involved.
var _ = uiregistry.RegisterBehavior(SortableListBehaviorName, sortablelistJS,
	uiregistry.Markers("[data-hui-sortable]"))
