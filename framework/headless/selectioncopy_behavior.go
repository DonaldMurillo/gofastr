package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed selectioncopy.js
var selectionCopyJS string

// SelectionCopyBehaviorName is the runtime module behind a selection's Copy
// control: it copies the checked rows as CSV.
const SelectionCopyBehaviorName = "headless-selection-copy"

var _ = uiregistry.RegisterBehavior(SelectionCopyBehaviorName, selectionCopyJS,
	uiregistry.Markers("[data-hui-selection-copy]"))
