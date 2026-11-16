package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed numbers.js
var numbersJS string

// NumbersBehaviorName is the runtime module that writes a number-mode
// signal (data-cui-signal-mode="number") in the page's locale.
const NumbersBehaviorName = "headless-numbers"

var _ = uiregistry.RegisterBehavior(NumbersBehaviorName, numbersJS,
	uiregistry.Markers(`[data-cui-signal-mode="number"]`))
