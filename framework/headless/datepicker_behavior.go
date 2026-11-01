package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed datepicker.js
var datepickerJS string

// DatePickerBehaviorName is the runtime module that binds the date
// picker's data-hui-* hooks: the grid keyboard, the pick into the
// native inputs, and focus across a month swap. The month swap itself
// is the kernel's data-cui-rpc contract.
const DatePickerBehaviorName = "headless-datepicker"

// The marker: the picker's root. Without script it is a native date
// input beside a calendar of real content whose month anchors are
// plain links, so the module loads only to add the pick and the keys.
// A day clicked while the module is still cold-fetching is replayed
// once it registers, so the first pick on a fresh page is not lost.
var _ = uiregistry.RegisterBehavior(DatePickerBehaviorName, datepickerJS,
	uiregistry.Markers("[data-hui-datepicker]"),
	uiregistry.Interactions(uiregistry.Interaction{
		Event:    "click",
		Selector: "[data-hui-date]",
	}))
