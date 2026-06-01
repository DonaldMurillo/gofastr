package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// signalToggleStyle registers the scoped CSS for fui-toggle. The runtime
// keeps aria-checked in sync via signal→attr mode, and the CSS reflects
// that attribute onto the track/thumb so the switch animates on click.
var signalToggleStyle = registry.RegisterStyle("fui-toggle", signalToggleCSS)

func signalToggleCSS(_ style.Theme) string {
	return `[data-fui-comp="fui-toggle"]{display:inline-flex;align-items:center;gap:.5rem;background:none;border:none;cursor:pointer;padding:0;color:var(--fui-foreground,#0f172a);font:inherit}` +
		`[data-fui-comp="fui-toggle"] .fui-toggle__track{position:relative;display:inline-block;width:2.5rem;height:1.375rem;border-radius:999px;background:var(--fui-border,#cbd5e1);transition:background .15s}` +
		`[data-fui-comp="fui-toggle"] .fui-toggle__thumb{position:absolute;top:2px;left:2px;width:1.125rem;height:1.125rem;border-radius:50%;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.2);transition:transform .15s}` +
		`[data-fui-comp="fui-toggle"][aria-checked="true"] .fui-toggle__track{background:var(--fui-primary,#3b82f6)}` +
		`[data-fui-comp="fui-toggle"][aria-checked="true"] .fui-toggle__thumb{transform:translateX(1.125rem)}` +
		`[data-fui-comp="fui-toggle"]:focus-visible{outline:2px solid var(--fui-primary,#3b82f6);outline-offset:2px;border-radius:4px}`
}

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
