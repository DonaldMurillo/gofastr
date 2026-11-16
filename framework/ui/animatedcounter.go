package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// AnimatedCounterConfig configures an AnimatedCounter.
type AnimatedCounterConfig struct {
	// Ctx sets the locale of the digits and default label; nil is English.
	Ctx context.Context

	// To is the target value, the number the server renders. Required.
	To int
	// From is the starting value during animation. Default 0.
	From int
	// DurationMs is the animation length. Default 1200.
	DurationMs int
	// Prefix and Suffix are static text on either side ("$", " users").
	Prefix string
	Suffix string
	ID     string
	Class  string
	// ExtraAttrs go on the root, less class, id, data-cui-* and data-hui-*.
	ExtraAttrs html.Attrs
}

var animatedCounterClasses = headless.Classes{
	headless.PartRoot:         "fui-animated-counter",
	headless.PartCounterValue: "fui-animated-counter__value",
}

// AnimatedCounter renders To, animated from From on first appearance.
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
	kids := []render.HTML{}
	if cfg.Prefix != "" {
		kids = append(kids, render.Tag("span",
			map[string]string{"class": "fui-animated-counter__prefix", "data-cui-internal": ""},
			render.Text(cfg.Prefix)))
	}
	inner := headless.Counter(headless.CounterProps{
		Signal:      sig,
		Value:       cfg.To,
		AnimateFrom: &from,
		DurationMS:  dur,
		Display:     true,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:       parts,
		Strings:     StringsFor(cfg.Ctx),
	}, animatedCounterClasses)
	kids = append(kids, headless.Own(inner))
	if cfg.Suffix != "" {
		kids = append(kids, render.Tag("span",
			map[string]string{"class": "fui-animated-counter__suffix", "data-cui-internal": ""},
			render.Text(cfg.Suffix)))
	}
	return animatedCounterStyle.WrapHTML(render.Tag("span",
		map[string]string{"class": "fui-animated-counter"}, kids...))
}

var animatedCounterStyle = registry.RegisterStyle("ui-animated-counter", animatedCounterCSS)

func animatedCounterCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-animated-counter"] {
  display: inline-flex;
  align-items: baseline;
  gap: var(--spacing-xs, 2px);
  font-variant-numeric: tabular-nums;
  font-weight: var(--font-weight-bold);
}
[data-cui-comp="ui-animated-counter"] .fui-animated-counter__value {
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-animated-counter"] .fui-animated-counter__prefix,
[data-cui-comp="ui-animated-counter"] .fui-animated-counter__suffix {
  color: var(--color-text-muted, #52525B);
  font-weight: var(--font-weight-semibold);
}`
}
