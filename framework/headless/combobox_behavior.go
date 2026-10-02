package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed combobox.js
var comboboxJS string

// ComboboxBehaviorName is the runtime module that binds the combobox's
// data-hui-* hooks and owns its keyboard contract. It replaces the
// retired core-ui/runtime combobox module; the RPC debouncing and the
// signal swap stay the kernel's data-fui-rpc contract.
const ComboboxBehaviorName = "headless-combobox"

// The marker: the input. A combobox without script is a labelled
// search form whose listbox is real content, so the module loads only
// to add the keyboard.
var _ = uiregistry.RegisterBehavior(ComboboxBehaviorName, comboboxJS,
	uiregistry.Markers("[data-hui-combobox-input]"))
