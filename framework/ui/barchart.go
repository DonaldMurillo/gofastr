package ui

import (
	"math"
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
//
// Legibility is built in, not opt-in: every bar carries its value above
// the cap so magnitudes read at a glance, the y-scale rounds up to a
// clean maximum so uniform / near-equal data keeps visible headroom (no
// wall of full-height slabs), a hairline baseline grounds the bars, and
// long category labels wrap onto multiple lines instead of truncating
// mid-word. See framework/docs/content/ui-new-components.md.

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
	// Width / Height in CSS pixels. Default 320×200.
	Width  int
	Height int
	// ShowAxis renders a left value axis: hairline gridlines at clean
	// tick values with numeric labels down the left gutter. Default off
	// (the always-on value labels + baseline already make magnitudes
	// legible; turn this on for a denser analytical read).
	ShowAxis bool
	// ShowLabels renders the category labels under each bar. Long labels
	// wrap onto up to two lines; a single over-long word is ellipsized
	// with the full text preserved in the bar's <title>. Default off.
	ShowLabels bool
	// HideValues suppresses the per-bar value labels that ride above each
	// cap. Values are shown by default — set this to opt out (e.g. a dense
	// sparkline-style strip where the numbers would crowd).
	HideValues bool
	// LabelledBy is the id of an element naming the chart for AT.
	LabelledBy string
	ID         string
	Class      string
}

// BarChart renders a categorical bar chart.
func BarChart(cfg BarChartConfig) render.HTML {
	if len(cfg.Bars) == 0 {
		return chartEmpty(cfg.Height, cfg.LabelledBy, cfg.Class, "No data yet")
	}
	w := cfg.Width
	if w == 0 {
		w = 320
	}
	h := cfg.Height
	if h == 0 {
		h = 200
	}

	var dataMax float64
	for _, b := range cfg.Bars {
		if b.Value < 0 {
			panic("ui: BarChart Bar.Value must be ≥0")
		}
		if b.Value > dataMax {
			dataMax = b.Value
		}
	}
	// Round the axis up to a clean maximum with ~15% headroom above the
	// tallest bar. This is what stops uniform (8/8/8/8) and near-equal
	// (6/5/5) datasets from rendering as identical full-height slabs — the
	// tallest bar lands around 85% of the plot, not the ceiling.
	valueMax := niceCeil(dataMax / 0.85)
	if valueMax == 0 {
		valueMax = 1
	}

	showValues := !cfg.HideValues

	n := len(cfg.Bars)

	// Layout gutters.
	axisGutter := 0.0 // left gutter for value-axis tick labels
	if cfg.ShowAxis {
		axisGutter = 30
	}
	topGutter := 0.0 // headroom for value labels above the caps
	if showValues {
		topGutter = 15
	}

	// Category-label band height depends on how many lines the labels wrap
	// to at this width. Compute the wrapped lines up front so the gutter
	// (and thus the plot height) accounts for them exactly.
	barGap := 8.0
	provPlotW := float64(w) - axisGutter
	provBarW := (provPlotW - barGap*float64(n-1)) / float64(n)
	slotChars := int(math.Floor((provBarW + barGap) / 6.2))
	var wrapped [][]string
	maxLines := 1
	if cfg.ShowLabels {
		wrapped = make([][]string, n)
		for i, b := range cfg.Bars {
			lines := wrapChartLabel(b.Label, slotChars)
			wrapped[i] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
	}
	lineH := 12.0
	bottomGutter := 0.0
	if cfg.ShowLabels {
		bottomGutter = float64(maxLines)*lineH + 4
	}

	plotW := float64(w) - axisGutter
	plotH := float64(h) - topGutter - bottomGutter
	if plotH < 8 {
		plotH = 8
	}
	baseY := topGutter + plotH

	totalGap := barGap * float64(n-1)
	barW := (plotW - totalGap) / float64(n)
	if barW < 3 {
		barW = 3
	}
	// Cap the bar thickness so a 1–2 category chart doesn't render giant
	// slabs; center the (capped) bars within their slots. The mark spec is
	// "never fill the slot; let the leftover be air".
	const maxBarW = 56.0
	slotW := barW + barGap
	drawW := barW
	if drawW > maxBarW {
		drawW = maxBarW
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
	sb.WriteString(escapeXML(cls))
	sb.WriteString(`" xmlns="http://www.w3.org/2000/svg"`)
	if cfg.ID != "" {
		sb.WriteString(` id="`)
		sb.WriteString(escapeXML(cfg.ID))
		sb.WriteString(`"`)
	}
	if cfg.LabelledBy != "" {
		sb.WriteString(` role="img" aria-labelledby="`)
		sb.WriteString(escapeXML(cfg.LabelledBy))
		sb.WriteString(`"`)
	} else {
		sb.WriteString(` aria-hidden="true"`)
	}
	sb.WriteString(` data-fui-comp="ui-bar-chart">`)

	// Value axis: hairline gridlines at clean ticks + left labels.
	if cfg.ShowAxis {
		ticks := niceTicks(valueMax)
		for _, tv := range ticks {
			ty := baseY - (tv/valueMax)*plotH
			sb.WriteString(`<line x1="`)
			sb.WriteString(ftoa(axisGutter))
			sb.WriteString(`" y1="`)
			sb.WriteString(ftoa(ty))
			sb.WriteString(`" x2="`)
			sb.WriteString(ftoa(float64(w)))
			sb.WriteString(`" y2="`)
			sb.WriteString(ftoa(ty))
			sb.WriteString(`" class="ui-bar-chart__grid"/>`)
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(axisGutter - 5))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ty + 3))
			sb.WriteString(`" class="ui-bar-chart__axis-label" text-anchor="end">`)
			sb.WriteString(ftoa(tv))
			sb.WriteString(`</text>`)
		}
	}

	// Baseline — always drawn so the bars sit on solid ground.
	sb.WriteString(`<line x1="`)
	sb.WriteString(ftoa(axisGutter))
	sb.WriteString(`" y1="`)
	sb.WriteString(ftoa(baseY))
	sb.WriteString(`" x2="`)
	sb.WriteString(ftoa(float64(w)))
	sb.WriteString(`" y2="`)
	sb.WriteString(ftoa(baseY))
	sb.WriteString(`" class="ui-bar-chart__baseline"/>`)

	for i, b := range cfg.Bars {
		slotX := axisGutter + float64(i)*slotW
		x := slotX + (barW-drawW)/2 // center capped bar in its slot
		cx := x + drawW/2
		barH := (b.Value / valueMax) * plotH
		// Keep a visible nub for tiny non-zero values so they don't vanish.
		if b.Value > 0 && barH < 2 {
			barH = 2
		}
		y := baseY - barH

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
		sb.WriteString(ftoa(drawW))
		sb.WriteString(`" height="`)
		sb.WriteString(ftoa(barH))
		sb.WriteString(`" rx="3" class="`)
		sb.WriteString(barCls)
		sb.WriteString(`"`)
		if !isPalette && color != "" {
			sb.WriteString(` fill="`)
			sb.WriteString(escapeXML(color))
			sb.WriteString(`"`)
		}
		if b.Label != "" {
			sb.WriteString(`><title>`)
			sb.WriteString(escapeXML(b.Label + ": " + ftoa(b.Value)))
			sb.WriteString(`</title></rect>`)
		} else {
			sb.WriteString(`/>`)
		}

		// Value label above the cap.
		if showValues {
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(y - 4))
			sb.WriteString(`" class="ui-bar-chart__value" text-anchor="middle">`)
			sb.WriteString(escapeXML(ftoa(b.Value)))
			sb.WriteString(`</text>`)
		}

		// Category label(s) under the bar — wrapped onto multiple lines.
		if cfg.ShowLabels && b.Label != "" {
			lines := wrapped[i]
			ly := baseY + lineH
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ly))
			sb.WriteString(`" class="ui-bar-chart__label" text-anchor="middle">`)
			for li, ln := range lines {
				sb.WriteString(`<tspan x="`)
				sb.WriteString(ftoa(cx))
				if li == 0 {
					sb.WriteString(`" dy="0">`)
				} else {
					sb.WriteString(`" dy="`)
					sb.WriteString(ftoa(lineH))
					sb.WriteString(`">`)
				}
				sb.WriteString(escapeXML(ln))
				sb.WriteString(`</tspan>`)
			}
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`</svg>`)
	return barChartStyle.WrapHTML(render.HTML(sb.String()))
}

// niceCeil rounds v up to a clean "nice" number (1/1.5/2/2.5/3/4/5/6/8 ×
// 10ⁿ) so axis maxima land on values a reader recognizes.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Floor(math.Log10(v))
	base := math.Pow(10, exp)
	f := v / base
	var nf float64
	switch {
	case f <= 1:
		nf = 1
	case f <= 1.5:
		nf = 1.5
	case f <= 2:
		nf = 2
	case f <= 2.5:
		nf = 2.5
	case f <= 3:
		nf = 3
	case f <= 4:
		nf = 4
	case f <= 5:
		nf = 5
	case f <= 6:
		nf = 6
	case f <= 8:
		nf = 8
	default:
		nf = 10
	}
	return nf * base
}

// niceTicks returns 0, max/2, max — three clean gridline values.
func niceTicks(max float64) []float64 {
	return []float64{0, max / 2, max}
}

// wrapChartLabel greedily word-wraps label to at most two lines that each
// fit within maxChars runes. A single word longer than a line is
// ellipsized; the full text is preserved by the caller in <title>.
func wrapChartLabel(label string, maxChars int) []string {
	if maxChars < 4 {
		maxChars = 4
	}
	if len([]rune(label)) <= maxChars {
		return []string{label}
	}
	words := strings.Fields(label)
	if len(words) == 0 {
		words = []string{label}
	}
	var lines []string
	cur := ""
	for _, wd := range words {
		cand := wd
		if cur != "" {
			cand = cur + " " + wd
		}
		if len([]rune(cand)) <= maxChars {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		cur = wd
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	// Hard-truncate any single over-long line (e.g. one huge word).
	for i := range lines {
		r := []rune(lines[i])
		if len(r) > maxChars {
			lines[i] = string(r[:maxChars-1]) + "…"
		}
	}
	if len(lines) > 2 {
		second := []rune(lines[1])
		if len(second) > maxChars-1 {
			second = second[:maxChars-1]
		}
		lines = []string{lines[0], string(second) + "…"}
	}
	return lines
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

[data-fui-comp="ui-bar-chart"] .ui-bar-chart__baseline {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__grid {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
  opacity: 0.55;
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__value {
  font-size: var(--text-xs, 0.72rem);
  font-weight: 600;
  fill: var(--color-text, #18181B);
  font-variant-numeric: tabular-nums;
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__label {
  font-size: var(--text-xs, 0.7rem);
  fill: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-bar-chart"] .ui-bar-chart__axis-label {
  font-size: 0.68rem;
  fill: var(--color-text-muted, #52525B);
  font-variant-numeric: tabular-nums;
}`
}
