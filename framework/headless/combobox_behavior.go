package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed combobox.js
var comboboxJS string

// ComboboxBehaviorName is the runtime module that binds the combobox's
// hooks and owns its keyboard contract.
const ComboboxBehaviorName = "headless-combobox"

var _ = uiregistry.RegisterBehavior(ComboboxBehaviorName, comboboxJS,
	uiregistry.Markers("[data-hui-combobox-input]"))
