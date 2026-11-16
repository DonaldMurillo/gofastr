package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed datepicker.js
var datepickerJS string

// DatePickerBehaviorName is the runtime module that binds the date
// picker's grid keyboard, the pick into the native inputs, and focus
// across a month swap.
const DatePickerBehaviorName = "headless-datepicker"

var _ = uiregistry.RegisterBehavior(DatePickerBehaviorName, datepickerJS,
	uiregistry.Markers("[data-hui-datepicker]"),
	uiregistry.Interactions(
		uiregistry.Interaction{Event: "click", Selector: "[data-hui-date]"},
		uiregistry.Interaction{Event: "click", Selector: "[data-hui-datepicker-nav]"},
	))
