package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// counterStyle registers the scoped CSS for fui-counter. The host emits
// it for any page whose HTML carries data-fui-comp="fui-counter".
var counterStyle = registry.RegisterStyle("fui-counter", counterCSS)

func counterCSS(_ style.Theme) string {
	return `[data-fui-comp="fui-counter"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
[data-fui-comp="fui-counter"]{display:inline-flex;align-items:center;gap:.5rem}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn{display:inline-flex;align-items:center;justify-content:center;width:2rem;height:2rem;border:1px solid var(--fui-border, var(--color-border, #e2e8f0));border-radius:.375rem;background:var(--fui-surface, var(--color-surface, #fff));color:var(--fui-foreground, var(--color-text, #0f172a));font-size:var(--text-lg, 1.125rem);line-height:1;cursor:pointer;transition:background .15s,border-color .15s}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn:hover{background:var(--fui-muted-bg, var(--color-surface-soft, #f1f5f9));border-color:var(--fui-primary, var(--color-primary, #3b82f6))}` +
		`[data-fui-comp="fui-counter"] .fui-counter__btn:focus-visible{outline:2px solid var(--fui-primary, var(--color-primary, #3b82f6));outline-offset:2px}` +
		`[data-fui-comp="fui-counter"] .fui-counter__value{min-width:2ch;text-align:center;font-variant-numeric:tabular-nums;font-weight:600;color:var(--fui-foreground, var(--color-text, #0f172a))}`
}

// CounterConfig configures a client-side counter with increment/decrement buttons.
// The counter is purely local, no RPC calls. It uses the signal system for state.
type CounterConfig struct {
	// SignalName is the signal that holds the count value. Required
	// unless Slice is set.
	SignalName string

	// Slice, when set, supplies both the signal name and the initial
	// value from one typed source (and auto-seeds it). Takes precedence
	// over SignalName.
	Slice *store.Slice[int]

	// Step is the increment/decrement size. Defaults to 1.
	Step int

	// Class is an optional extra CSS class on the wrapper.
	Class string
	// Ctx carries the per-request context used to resolve the Decrement,
	// Increment and Counter group aria labels. When nil, English fallbacks apply.
	Ctx context.Context

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root element. Keys
	// the component owns are dropped: class (use Class), id, and
	// data-fui-*, plus role=group and the aria-label it derives.
	ExtraAttrs html.Attrs
}

// Counter renders a counter with + and − buttons that mutate a signal
// locally in the browser. No server round-trip.
//
// The counter displays a `<span data-fui-signal="name">0</span>` that
// the runtime updates when the signal changes.
func Counter(cfg CounterConfig) render.HTML {
	name := cfg.SignalName
	initial := 0
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		initial = cfg.Slice.Default()
	}
	if name == "" {
		panic("ui: Counter requires SignalName or Slice")
	}

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}
	return counterStyle.WrapHTML(headless.Counter(headless.CounterProps{
		Signal:     name,
		Value:      initial,
		Step:       cfg.Step,
		ID:         autoID("counter"),
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:      parts,
		Strings:    StringsFor(ctx),
	}, counterClasses))
}

// counterClasses dresses headless.Counter's parts in this package's
// own vocabulary — the names the registered fui-counter sheet
// matches.
var counterClasses = headless.Classes{
	headless.PartRoot:             "fui-counter",
	headless.PartCounterDecrement: "fui-counter__btn fui-counter__dec",
	headless.PartCounterValue:     "fui-counter__value",
	headless.PartCounterIncrement: "fui-counter__btn fui-counter__inc",
}
