package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── BarChart ───────────────────────────────────────────────────────
//
// Pure-SVG categorical bar chart. No JS, no hydration. Bars use the
// theme primary by default; per-bar Color overrides apply.

// BarChartBar is one bar.
type BarChartBar struct {
	// Label is the x-axis category label + AT <title>.
	Label string
	// Value is the bar height (≥0).
	Value float64
	// Color overrides the default theme primary. Optional CSS color
	// or one of: "primary", "info", "success", "warning", "danger".
	Color string
}

// BarChartConfig configures a BarChart.
type BarChartConfig struct {
	// Bars are the entries (≥1).
	Bars []BarChartBar
	// Width / Height in CSS pixels. Default 320×180.
	Width  int
	Height int
	// ShowAxis renders thin x/y axis lines + min/max value labels on
	// the left. Default off (compact mode).
	ShowAxis bool
	// ShowLabels renders the bar labels under the chart. Default on.
	ShowLabels bool
	// LabelledBy is the id of an element naming the chart for AT.
	LabelledBy string
	ID         string
	Class      string
}

// BarChart renders a categorical bar chart.
func BarChart(cfg BarChartConfig) render.HTML {
	if len(cfg.Bars) == 0 {
		panic("ui: BarChart requires ≥1 Bar")
	}
	w := cfg.Width
	if w == 0 {
		w = 320
	}
	h := cfg.Height
	if h == 0 {
		h = 180
	}
	axisGutter := 0
	bottomGutter := 0
	if cfg.ShowAxis {
		axisGutter = 32
	}
	if cfg.ShowLabels {
		bottomGutter = 18
	}
	if cfg.ShowLabels && cfg.ShowAxis {
		// Combined doesn't double-stack; share the bottom gutter.
		bottomGutter = 28
	}

	plotW := float64(w - axisGutter)
	plotH := float64(h - bottomGutter)

	var max float64
	for _, b := range cfg.Bars {
		if b.Value < 0 {
			panic("ui: BarChart Bar.Value must be ≥0")
		}
		if b.Value > max {
			max = b.Value
		}
	}
	if max == 0 {
		max = 1
	}

	n := len(cfg.Bars)
	barGap := 6
	totalGap := float64(barGap * (n - 1))
	barW := (plotW - totalGap) / float64(n)
	if barW < 4 {
		barW = 4
	}

	cls := "ui-bar-chart"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	var sb strings.Builder
	sb.WriteString(`<svg width="`)
	sb.WriteString(strconv.Itoa(w))
	sb.WriteString(`" height="`)
	sb.WriteString(strconv.Itoa(h))
	sb.WriteString(`" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(w))
	sb.WriteString(` `)
	sb.WriteString(strconv.Itoa(h))
	sb.WriteString(`" class="`)
	sb.WriteString(cls)
	sb.WriteString(`" xmlns="http://www.w3.org/2000/svg"`)
	if cfg.ID != "" {
		sb.WriteString(` id="`)
		sb.WriteString(cfg.ID)
		sb.WriteString(`"`)
	}
	if cfg.LabelledBy != "" {
		sb.WriteString(` role="img" aria-labelledby="`)
		sb.WriteString(cfg.LabelledBy)
		sb.WriteString(`"`)
	} else {
		sb.WriteString(` aria-hidden="true"`)
	}
	sb.WriteString(` data-fui-comp="ui-bar-chart">`)

	// Axis lines (if requested).
	if cfg.ShowAxis {
		// y-axis label values (0 and max).
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(float64(axisGutter) - 4))
		sb.WriteString(`" y="14" class="ui-bar-chart__axis-label" text-anchor="end">`)
		sb.WriteString(ftoa(max))
		sb.WriteString(`</text>`)
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(float64(axisGutter) - 4))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(plotH - 2))
		sb.WriteString(`" class="ui-bar-chart__axis-label" text-anchor="end">0</text>`)
	}

	for i, b := range cfg.Bars {
		x := float64(axisGutter) + float64(i)*(barW+float64(barGap))
		barH := (b.Value / max) * plotH
		y := plotH - barH

		color := b.Color
		barCls := "ui-bar-chart__bar"
		isPalette := color == "primary" || color == "info" ||
			color == "success" || color == "warning" || color == "danger"
		if color == "" {
			barCls += " ui-bar-chart__bar--primary"
		} else if isPalette {
			barCls += " ui-bar-chart__bar--" + color
		}

		sb.WriteString(`<rect x="`)
		sb.WriteString(ftoa(x))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(y))
		sb.WriteString(`" width="`)
		sb.WriteString(ftoa(barW))
		sb.WriteString(`" height="`)
		sb.WriteString(ftoa(barH))
		sb.WriteString(`" rx="2" class="`)
		sb.WriteString(barCls)
		sb.WriteString(`"`)
		if !isPalette && color != "" {
			sb.WriteString(` fill="`)
			sb.WriteString(color)
			sb.WriteString(`"`)
		}
		if b.Label != "" {
			sb.WriteString(`><title>`)
			sb.WriteString(escapeXML(b.Label + ": " + ftoa(b.Value)))
			sb.WriteString(`</title></rect>`)
		} else {
			sb.WriteString(`/>`)
		}

		if cfg.ShowLabels && b.Label != "" {
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(x + barW/2))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(plotH + 12))
			sb.WriteString(`" class="ui-bar-chart__label" text-anchor="middle">`)
			sb.WriteString(escapeXML(b.Label))
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`</svg>`)
	return barChartStyle.WrapHTML(render.HTML(sb.String()))
}

var barChartStyle = registry.RegisterStyle("ui-bar-chart", barChartCSS)

func barChartCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-bar-chart"] {
  display: block;
  max-inline-size: 100%;
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__bar {
  transition: opacity 120ms ease;
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__bar:hover {
  opacity: 0.85;
}
.ui-bar-chart__bar--primary { fill: var(--color-primary, #4F46E5); }
.ui-bar-chart__bar--info    { fill: var(--color-info, #3B82F6); }
.ui-bar-chart__bar--success { fill: var(--color-success, #16A34A); }
.ui-bar-chart__bar--warning { fill: var(--color-warning, #D97706); }
.ui-bar-chart__bar--danger  { fill: var(--color-danger, #DC2626); }

[data-fui-comp="ui-bar-chart"] .ui-bar-chart__label {
  font-size: 0.7rem;
  fill: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__axis-label {
  font-size: 0.7rem;
  fill: var(--color-text-muted, #52525B);
  font-variant-numeric: tabular-nums;
}`
}
