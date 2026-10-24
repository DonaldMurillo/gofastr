package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed numbers.js
var numbersJS string

// NumbersBehaviorName is the runtime module that writes a number-mode
// signal (data-cui-signal-mode="number") the way the page's language
// writes numbers, so a counter's live value matches the i18n.Number
// text the server rendered. The host serves it at
// /__gofastr/runtime/headless-numbers.js; it lives here, not in the
// core runtime, so a page with no live number pays nothing for it.
const NumbersBehaviorName = "headless-numbers"

var _ = uiregistry.RegisterBehavior(NumbersBehaviorName, numbersJS,
	uiregistry.Markers(`[data-cui-signal-mode="number"]`))
