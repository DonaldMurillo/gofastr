package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── AnimatedCounter ────────────────────────────────────────────────
//
// A number that ticks from a start value up to its target, over
// headless.Counter with AnimateFrom set: the SSR-rendered number IS
// the final value (users without JS or with reduced motion see the
// right number), and the headless-controls module drives the brief
// animation on first appearance.

// AnimatedCounterConfig configures an AnimatedCounter.
type AnimatedCounterConfig struct {
	// To is the target value (required).
	To int
	// From is the starting value during animation. Default 0.
	From int
	// DurationMs is the animation length. Default 1200.
	DurationMs int
	// Prefix / Suffix are static strings on either side (e.g.
	// Prefix="$", Suffix="+", Suffix=" users").
	Prefix string
	Suffix string
	// ID / Class are passed through.
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the counter's root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs
}

// animatedCounterClasses dresses headless.Counter's parts in this
// package's own vocabulary — the names the registered
// ui-animated-counter sheet matches.
var animatedCounterClasses = headless.Classes{
	headless.PartRoot:         "fui-animated-counter",
	headless.PartCounterValue: "fui-animated-counter__value",
}

// AnimatedCounter renders a number that ticks from From to To on
// first appearance. The prefix and suffix are slots around the value
// the primitive owns.
func AnimatedCounter(cfg AnimatedCounterConfig) render.HTML {
	dur := cfg.DurationMs
	if dur == 0 {
		dur = 1200
	}
	from := cfg.From
	sig := "animated-counter-" + strconv.Itoa(cfg.To)
	parts := headless.Parts{}
	if extra := strings.TrimSpace(cfg.Class); extra != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": extra}}
	}
	// The prefix and suffix are this layer's presentation around the
	// value the primitive renders; they travel as part attrs on the
	// root's own spans so the sheet can tell them apart.
	kids := []render.HTML{}
	if cfg.Prefix != "" {
		kids = append(kids, render.Tag("span",
			map[string]string{"class": "fui-animated-counter__prefix"},
			render.Text(cfg.Prefix)))
	}
	inner := headless.Counter(headless.CounterProps{
		Signal:      sig,
		Value:       cfg.To,
		AnimateFrom: &from,
		DurationMS:  dur,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:       parts,
		Strings:     StringsFor(nil),
	}, animatedCounterClasses)
	kids = append(kids, inner)
	if cfg.Suffix != "" {
		kids = append(kids, render.Tag("span",
			map[string]string{"class": "fui-animated-counter__suffix"},
			render.Text(cfg.Suffix)))
	}
	return animatedCounterStyle.WrapHTML(render.Tag("span",
		map[string]string{"class": "fui-animated-counter"}, kids...))
}

var animatedCounterStyle = registry.RegisterStyle("ui-animated-counter", animatedCounterCSS)

func animatedCounterCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-animated-counter"] {
  display: inline-flex;
  align-items: baseline;
  gap: var(--spacing-xs, 2px);
  font-variant-numeric: tabular-nums;
  font-weight: 700;
}
[data-fui-comp="ui-animated-counter"] .fui-animated-counter__value {
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-animated-counter"] .fui-animated-counter__prefix,
[data-fui-comp="ui-animated-counter"] .fui-animated-counter__suffix {
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
}`
}
