package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── SignalToggle ────────────────────────────────────────────────────

// SignalToggleConfig configures a boolean toggle/switch that flips a
// signal entirely client-side. Unlike the form-based Switch (which
// wraps a native <input type="checkbox">), SignalToggle uses the
// runtime's signal system — no form submission, pure JS reactivity.
//
// Clicking the button toggles the named signal; the signal drives both
// the aria-checked attribute and a visible label.
type SignalToggleConfig struct {
	SignalName string // required — the boolean signal name
	Label      string // optional — aria-label (falls back to SignalName)
	Class      string // optional — extra CSS classes
}

// SignalToggle renders a <button role="switch"> that toggles a boolean
// signal on click. The signal binding is fully client-side:
//
//   - data-fui-signal-toggle flips the signal on click
//   - data-fui-signal + attr mode keeps aria-checked in sync
//   - a nested label span shows the signal value via data-fui-signal
//
// The button carries data-fui-comp="fui-toggle" for scoped CSS auto-loading.
func SignalToggle(cfg SignalToggleConfig) render.HTML {
	if cfg.SignalName == "" {
		panic("ui: SignalToggle requires SignalName")
	}
	label := cfg.Label
	if label == "" {
		label = cfg.SignalName
	}
	cls := "fui-toggle"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Build inner children as a single HTML string — static structure.
	inner := fmt.Sprintf(
		`<span class="fui-toggle__track"><span class="fui-toggle__thumb"></span></span>`+
			`<span class="fui-toggle__label" data-fui-signal="%s">false</span>`,
		cfg.SignalName,
	)

	// Construct the full button element.
	// data-fui-signal-toggle — click handler flips the signal
	// data-fui-signal + data-fui-signal-mode="attr" — binds signal value to aria-checked
	return render.HTML(fmt.Sprintf(
		`<button class="%s" data-fui-comp="fui-toggle"`+
			` data-fui-signal-toggle="%s"`+
			` data-fui-signal="%s" data-fui-signal-mode="attr" data-fui-signal-attr="aria-checked"`+
			` role="switch" aria-checked="false" aria-label="%s">%s</button>`,
		cls, cfg.SignalName, cfg.SignalName, label, inner,
	))
}
