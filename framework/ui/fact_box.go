package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FactStyle picks the visual hierarchy of a FactBox.
type FactStyle string

const (
	// FactStyleLabelFirst puts a small muted label above the value. The default.
	FactStyleLabelFirst FactStyle = ""
	// FactStyleValueFirst puts a large display value above the label, for KPI bands.
	FactStyleValueFirst FactStyle = "value-first"
)

// FactBoxConfig configures one labelled fact.
type FactBoxConfig struct {
	Label string // required short label
	Value string // visible value; mutually exclusive with ValueHTML
	// ValueHTML is the value as inline markup; it wins over Value.
	ValueHTML render.HTML
	// Style picks the visual hierarchy. Default FactStyleLabelFirst.
	Style FactStyle
	// FullWidth spans the box across its grid row.
	FullWidth bool
	ID        string
	Class     string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// FactBox renders one labelled value tile.
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
			`. Pick "" (label-first) or "value-first"`)
	}
	cls := "fui-fact-box"
	if cfg.Style != FactStyleLabelFirst {
		cls += " fui-fact-box--" + string(cfg.Style)
	}
	if cfg.FullWidth {
		cls += " fui-fact-box--full"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	value := cfg.ValueHTML
	if value == "" {
		value = render.Text(cfg.Value)
	}
	label := html.Span(html.TextConfig{Class: "fui-fact-box__label", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(cfg.Label))
	var valAttrs html.Attrs
	if cfg.ValueHTML == "" {
		valAttrs = html.Attrs{"data-cui-internal": ""}
	}
	val := html.Span(html.TextConfig{Class: "fui-fact-box__value", ExtraAttrs: valAttrs}, value)
	children := []render.HTML{label, val}
	if cfg.Style == FactStyleValueFirst {
		children = []render.HTML{val, label}
	}
	return factBoxStyle.WrapHTML(html.Div(html.DivConfig{
		Class: cls, ID: cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, children...))
}

var factBoxStyle = registry.RegisterStyle("ui-fact-box", factBoxCSS)

func factBoxCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-fact-box"] {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, transparent);
}
:where([data-cui-comp="ui-fact-box"]).fui-fact-box--full {
  grid-column: 1 / -1;
}
[data-cui-comp="ui-fact-box"] .fui-fact-box__label {
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-muted, currentColor);
}
[data-cui-comp="ui-fact-box"] .fui-fact-box__value {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, currentColor);
  line-height: var(--leading-normal, 1.5);
}

/* Value-first variant: big display value on top, label as caption
   underneath. Stat-band/KPI use cases. */
[data-cui-comp="ui-fact-box"].fui-fact-box--value-first {
  border: 0;
  background: transparent;
  padding: 0;
}
[data-cui-comp="ui-fact-box"].fui-fact-box--value-first .fui-fact-box__value {
  font-size: var(--ui-fact-box-value-size, 32px);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  color: var(--ui-fact-box-value-color, var(--color-primary, currentColor));
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-fact-box"].fui-fact-box--value-first .fui-fact-box__label {
  font-size: var(--text-xs, 12px);
}`
}
