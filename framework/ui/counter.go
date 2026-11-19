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

var counterStyle = registry.RegisterStyle("fui-counter", counterCSS)

func counterCSS(_ style.Theme) string {
	return `[data-cui-comp="fui-counter"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
[data-cui-comp="fui-counter"]{display:inline-flex;align-items:center;gap:var(--spacing-md, 8px)}` +
		`[data-cui-comp="fui-counter"] .fui-counter__btn{display:inline-flex;align-items:center;justify-content:center;width:var(--ui-counter-btn-size, 2rem);height:var(--ui-counter-btn-size, 2rem);border:var(--stroke-thin, 1px) solid var(--fui-border, var(--color-border, #E4E4E7));border-radius:var(--radii-sm, 6px);background:var(--fui-surface, var(--color-surface, #fff));color:var(--fui-foreground, var(--color-text, #09090B));font-size:var(--text-lg, 1.125rem);line-height:1;cursor:pointer;transition:background var(--duration-fast, 150ms),border-color var(--duration-fast, 150ms)}` +
		`[data-cui-comp="fui-counter"] .fui-counter__btn:hover{background:var(--fui-muted-bg, var(--color-surface-soft, #F4F4F5));border-color:var(--fui-primary, var(--color-primary, #18181B))}` +
		`[data-cui-comp="fui-counter"] .fui-counter__btn:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:var(--stroke-focus-offset, 2px)}` +
		`[data-cui-comp="fui-counter"] .fui-counter__value{min-width:2ch;text-align:center;font-variant-numeric:tabular-nums;font-weight:var(--font-weight-semibold);color:var(--fui-foreground, var(--color-text, #09090B))}`
}

// CounterConfig configures a Counter.
type CounterConfig struct {
	// SignalName is the signal holding the count. Required unless Slice is set.
	SignalName string

	// Slice supplies the signal name and the request's value, and wins
	// over SignalName.
	Slice *store.Slice[int]

	// Step is the change per press. Default 1.
	Step int

	Class string
	// Ctx sets the locale of the buttons' and group's labels; nil is English.
	Ctx context.Context

	// ExtraAttrs go on the root, less class, id, data-cui-*, role and aria-label.
	ExtraAttrs html.Attrs
}

// Counter renders a value with + and − buttons that change a signal in
// the browser, with no server round-trip.
func Counter(cfg CounterConfig) render.HTML {
	name := cfg.SignalName
	initial := 0
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		initial = cfg.Slice.Value(cfg.Ctx)
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
		Seeded:     true,
		Step:       cfg.Step,
		ID:         scopedID(cfg.Ctx, "counter"),
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:      parts,
		Strings:    StringsFor(ctx),
	}, counterClasses))
}

var counterClasses = headless.Classes{
	headless.PartRoot:             "fui-counter",
	headless.PartCounterDecrement: "fui-counter__btn fui-counter__dec",
	headless.PartCounterValue:     "fui-counter__value",
	headless.PartCounterIncrement: "fui-counter__btn fui-counter__inc",
}
