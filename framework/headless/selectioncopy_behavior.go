package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed selectioncopy.js
var selectionCopyJS string

// SelectionCopyBehaviorName is the runtime module behind a selection's
// Copy control (SelectionProps.CopyURL): it fetches the checked rows as
// CSV and writes them to the clipboard.
const SelectionCopyBehaviorName = "headless-selection-copy"

// The marker: the Copy control.
var _ = uiregistry.RegisterBehavior(SelectionCopyBehaviorName, selectionCopyJS,
	uiregistry.Markers("[data-hui-selection-copy]"))
