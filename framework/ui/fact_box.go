package ui

// FactBox — one labelled value tile. Single component, two variants:
//
//   FactStyleLabelFirst (default) — small uppercase label on top, body-size
//     value below. For parameters and specs ("Prereqs: Go 1.26+, git").
//   FactStyleValueFirst — big display value on top, small caption label
//     below. For KPIs and counts ("53 docs").
//
// Two render functions previously (FactBox + StatTile) were collapsed
// into one because they differ only in source order and value typography.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FactStyle picks the visual hierarchy of a FactBox.
type FactStyle string

const (
	// FactStyleLabelFirst renders the label on top (small, uppercase)
	// and the value below (body-size). Default.
	FactStyleLabelFirst FactStyle = ""
	// FactStyleValueFirst renders the value on top (large display
	// type) and the label below (small, uppercase). Use for KPI-style
	// stat bands.
	FactStyleValueFirst FactStyle = "value-first"
)

// FactBoxConfig configures one labelled fact.
type FactBoxConfig struct {
	Label string // required short label
	Value string // visible value; mutually exclusive with ValueHTML
	// ValueHTML lets the value contain inline markup (code, links).
	// If non-empty, takes precedence over Value.
	ValueHTML render.HTML
	// Style picks the visual hierarchy. Default FactStyleLabelFirst.
	Style FactStyle
	// FullWidth, when true, marks the box to span the full grid row
	// (the consuming grid still controls the column template).
	FullWidth bool
	ID        string
	Class     string
}

// FactBox renders one labelled tile. Pair with a CSS grid to lay out
// multiple — the framework does not provide a grid wrapper, so
// consumers pick their own column template.
func FactBox(cfg FactBoxConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: FactBox requires Label")
	}
	if cfg.Value == "" && cfg.ValueHTML == "" {
		panic("ui: FactBox requires Value or ValueHTML")
	}
	switch cfg.Style {
	case FactStyleLabelFirst, FactStyleValueFirst:
	default:
		panic("ui: FactBox unknown Style " + string(cfg.Style) +
			` — pick "" (label-first) or "value-first"`)
	}
	cls := "ui-fact-box"
	if cfg.Style != FactStyleLabelFirst {
		cls += " ui-fact-box--" + string(cfg.Style)
	}
	if cfg.FullWidth {
		cls += " ui-fact-box--full"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	value := cfg.ValueHTML
	if value == "" {
		value = render.Text(cfg.Value)
	}
	label := html.Span(html.TextConfig{Class: "ui-fact-box__label"}, render.Text(cfg.Label))
	val := html.Span(html.TextConfig{Class: "ui-fact-box__value"}, value)
	children := []render.HTML{label, val}
	if cfg.Style == FactStyleValueFirst {
		children = []render.HTML{val, label}
	}
	return factBoxStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, children...))
}

var factBoxStyle = registry.RegisterStyle("ui-fact-box", factBoxCSS)

func factBoxCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-fact-box"] {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 4px);
  padding: var(--spacing-md, 16px);
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-soft, transparent);
}
[data-fui-comp="ui-fact-box"].ui-fact-box--full {
  grid-column: 1 / -1;
}
[data-fui-comp="ui-fact-box"] .ui-fact-box__label {
  font-size: var(--text-xs, 11px);
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--color-text-subtle, currentColor);
}
[data-fui-comp="ui-fact-box"] .ui-fact-box__value {
  font-size: var(--font-size-md, 14px);
  color: var(--color-text, currentColor);
  line-height: 1.5;
}

/* Value-first variant: big display value on top, label as caption
   underneath. Stat-band/KPI use cases. */
[data-fui-comp="ui-fact-box"].ui-fact-box--value-first {
  border: 0;
  background: transparent;
  padding: 0;
}
[data-fui-comp="ui-fact-box"].ui-fact-box--value-first .ui-fact-box__value {
  font-size: var(--ui-fact-box-value-size, 32px);
  font-weight: 600;
  line-height: 1;
  color: var(--ui-fact-box-value-color, var(--color-primary, currentColor));
  font-variant-numeric: tabular-nums;
}
[data-fui-comp="ui-fact-box"].ui-fact-box--value-first .ui-fact-box__label {
  font-size: var(--text-xs, 12px);
}`
}
