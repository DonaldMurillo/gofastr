package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// counterStyle registers the scoped CSS for fui-counter. The host emits
// it for any page whose HTML carries data-fui-comp="fui-counter".
var counterStyle = registry.RegisterStyle("fui-counter", counterCSS)

func counterCSS(_ style.Theme) string {
	return `[data-fui-comp="fui-counter"]{display:inline-flex;align-items:center;gap:.5rem}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn{display:inline-flex;align-items:center;justify-content:center;width:2rem;height:2rem;border:1px solid var(--fui-border,#e2e8f0);border-radius:.375rem;background:var(--fui-surface,#fff);color:var(--fui-foreground,#0f172a);font-size:1.125rem;line-height:1;cursor:pointer;transition:background .15s,border-color .15s}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn:hover{background:var(--fui-muted-bg,#f1f5f9);border-color:var(--fui-primary,#3b82f6)}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn:focus-visible{outline:2px solid var(--fui-primary,#3b82f6);outline-offset:2px}` +
		`[data-fui-comp="fui-counter"] .fui-counter__value{min-width:2ch;text-align:center;font-variant-numeric:tabular-nums;font-weight:600;color:var(--fui-foreground,#0f172a)}`
}

// CounterConfig configures a client-side counter with increment/decrement buttons.
// The counter is purely local — no RPC calls. It uses the signal system for state.
type CounterConfig struct {
	// SignalName is the signal that holds the count value. Required.
	SignalName string

	// Step is the increment/decrement size. Defaults to 1.
	Step int

	// Class is an optional extra CSS class on the wrapper.
	Class string
}

// Counter renders a counter with + and − buttons that mutate a signal
// locally in the browser. No server round-trip.
//
// The counter displays a `<span data-fui-signal="name">0</span>` that
// the runtime updates when the signal changes.
func Counter(cfg CounterConfig) render.HTML {
	if cfg.SignalName == "" {
		panic("ui: Counter requires SignalName")
	}
	step := cfg.Step
	if step == 0 {
		step = 1
	}

	cls := "fui-counter"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	decBtn := render.Tag("button", map[string]string{
		"class":               "fui-counter__btn fui-counter__dec",
		"data-fui-signal-inc": cfg.SignalName + ":" + strconv.Itoa(-step),
		"aria-label":          "Decrement",
		"type":                "button",
	}, render.Text("−"))

	display := render.Tag("span", map[string]string{
		"class":           "fui-counter__value",
		"data-fui-signal": cfg.SignalName,
		"aria-live":       "polite",
	}, render.Text("0"))

	incAttrs := map[string]string{
		"class":      "fui-counter__btn fui-counter__inc",
		"aria-label": "Increment",
		"type":       "button",
	}
	if step == 1 {
		incAttrs["data-fui-signal-inc"] = cfg.SignalName
	} else {
		incAttrs["data-fui-signal-inc"] = cfg.SignalName + ":" + strconv.Itoa(step)
	}
	incBtn := render.Tag("button", incAttrs, render.Text("+"))

	return render.Tag("div", map[string]string{
		"class":         cls,
		"data-fui-comp": "fui-counter",
		"role":          "group",
		"aria-label":    "Counter",
	}, decBtn, display, incBtn)
}
