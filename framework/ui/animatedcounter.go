package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── AnimatedCounter ────────────────────────────────────────────────
//
// A number that ticks from a start value up to a target on first
// IntersectionObserver hit (so it animates when scrolled into view,
// not pre-scroll). The SSR-rendered number is the final value so
// users without JS / motion-reduced users still see the right
// number; the runtime hooks in to drive the brief animation.

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
	// ID / Class / Attrs are passed through.
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// AnimatedCounter renders a number that ticks from From to To on
// first appearance.
func AnimatedCounter(cfg AnimatedCounterConfig) render.HTML {
	dur := cfg.DurationMs
	if dur == 0 {
		dur = 1200
	}
	cls := "ui-animated-counter"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{
		"class":                          cls,
		"data-fui-animated-counter":      strconv.Itoa(cfg.To),
		"data-fui-animated-counter-from": strconv.Itoa(cfg.From),
		"data-fui-animated-counter-ms":   strconv.Itoa(dur),
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	children := []render.HTML{}
	if cfg.Prefix != "" {
		children = append(children,
			html.Span(html.TextConfig{Class: "ui-animated-counter__prefix"}, render.Text(cfg.Prefix)))
	}
	children = append(children,
		html.Span(html.TextConfig{Class: "ui-animated-counter__value"}, render.Text(strconv.Itoa(cfg.To))))
	if cfg.Suffix != "" {
		children = append(children,
			html.Span(html.TextConfig{Class: "ui-animated-counter__suffix"}, render.Text(cfg.Suffix)))
	}

	return animatedCounterStyle.WrapHTML(render.Tag("span", attrs, children...))
}

var animatedCounterStyle = registry.RegisterStyle("ui-animated-counter", animatedCounterCSS)

func animatedCounterCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-animated-counter"] {
  display: inline-flex;
  align-items: baseline;
  gap: 2px;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
}
[data-fui-comp="ui-animated-counter"] .ui-animated-counter__value {
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-animated-counter"] .ui-animated-counter__prefix,
[data-fui-comp="ui-animated-counter"] .ui-animated-counter__suffix {
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
}`
}
