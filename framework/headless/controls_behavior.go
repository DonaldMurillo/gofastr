package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed controls.js
var controlsJS string

// ControlsBehaviorName is the runtime module that binds the stateful
// form controls' data-hui-* hooks. The host serves it at
// /__gofastr/runtime/headless-controls.js and the kernel loads it when
// one of its markers is on the page; it replaces the retired
// core-ui/runtime numberinput, slider, rangeslider and animatedcounter
// modules.
const ControlsBehaviorName = "headless-controls"

// The markers are spelled as literals on the call because the runtime's
// hard-rule-5 gate reads every registry.Markers call in the tree. One
// per behaviour: the animated counter's root (an unanimated counter
// needs no module — the signals kernel is the whole increment path),
// the stepper's root, the slider's output (a bare slider carries no
// mirror and no module), and the range pair's root (its cross-clamp
// matters even without an output).
var _ = uiregistry.RegisterBehavior(ControlsBehaviorName, controlsJS,
	uiregistry.Markers("[data-hui-counter-animate]", "[data-hui-number-input-decrement]",
		"[data-hui-slider-output]", "[data-hui-range-slider]"))
