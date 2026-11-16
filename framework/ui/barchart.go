package ui

import (
	"context"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// BarChartBar is one bar.
type BarChartBar struct {
	// Label is the category label and the bar's <title>.
	Label string
	// Value is the bar's height, at least 0.
	Value float64
	// Color is a palette token, a registered status variant, or a CSS
	// color as hex, rgb(), hsl(), oklch(), color() or var(--…); anything
	// else is the theme primary.
	Color string
}

// BarChartConfig configures a BarChart.
type BarChartConfig struct {
	Bars []BarChartBar
	// Width and Height in CSS pixels. Default 320×200.
	Width  int
	Height int
	// ShowAxis draws gridlines and value labels down a left gutter.
	ShowAxis bool
	// ShowLabels draws the category labels, wrapped onto two lines.
	ShowLabels bool
	// Max fixes the top of the scale (100 for a percentage); zero rounds
	// up from the data, and a taller bar grows it.
	Max float64
	// HideValues drops the value label above each bar.
	HideValues bool
	// FitHeight sizes the SVG so the tallest bar is 96px, unless Height is set.
	FitHeight bool
	// LabelledBy names the chart; without it the chart is aria-hidden.
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs go on the root <svg>, less the keys the chart derives.
	ExtraAttrs html.Attrs
	// Ctx sets the locale of numbers and words; nil is English.
	Ctx context.Context
}

const (
	axisLabelHalf  = 10.0
	axisPhoneScale = 16.0 / 12.0
)

// BarChart renders a categorical bar chart.
func BarChart(cfg BarChartConfig) render.HTML {
	if len(cfg.Bars) == 0 {
		return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, "", cfg.ExtraAttrs)
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
		if !finite(b.Value) {
			continue
		}
		if b.Value < 0 {
			panic("ui: BarChart Bar.Value must be ≥0")
		}
		if b.Value > dataMax {
			dataMax = b.Value
		}
	}
	valueMax := niceCeil(dataMax / 0.85)
	if !finite(valueMax) || valueMax <= 0 {
		valueMax = dataMax
	}
	if finite(cfg.Max) && cfg.Max > 0 {
		valueMax = math.Max(cfg.Max, dataMax)
	}
	if valueMax == 0 {
		valueMax = 1
	}

	showValues := !cfg.HideValues

	n := len(cfg.Bars)

	axisGutter := 0.0
	if cfg.ShowAxis {
		axisGutter = 30
		for _, tv := range niceTicks(valueMax) {
			need := float64(utf8.RuneCountInString(chartNumber(cfg.Ctx, tv)))*7*axisPhoneScale + 8
			axisGutter = math.Max(axisGutter, math.Ceil(need))
		}
	}
	topGutter := 0.0
	if showValues {
		topGutter = 15
	}

	provPlotW := float64(w) - axisGutter
	provBarW, barGap := barSlots(provPlotW, n)
	slotChars := int(math.Floor((provBarW + barGap - 4) / 7))
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
	const labelGap = 6.0
	bottomGutter := 0.0
	if cfg.ShowLabels {
		bottomGutter = float64(maxLines)*lineH + labelGap + 4
	}

	if cfg.ShowAxis {
		topGutter = math.Max(topGutter, axisLabelHalf)
		bottomGutter = math.Max(bottomGutter, axisLabelHalf)
	}

	plotH := float64(h) - topGutter - bottomGutter
	if cfg.FitHeight && cfg.Height == 0 {
		const fitTallestBar = 96.0
		if dataMax > 0 {
			plotH = fitTallestBar * (valueMax / dataMax)
		} else {
			plotH = fitTallestBar
		}
		h = int(math.Round(topGutter + plotH + bottomGutter))
	}
	if plotH < 8 {
		plotH = 8
	}
	baseY := topGutter + plotH

	barW := provBarW
	const maxBarW = 56.0
	slotW := barW + barGap
	drawW := barW
	if drawW > maxBarW {
		drawW = maxBarW
	}

	cls := "fui-bar-chart"
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
	sb.WriteString(` data-cui-comp="ui-bar-chart"`)
	sb.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs,
		"width", "height", "viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden")))
	sb.WriteString(`>`)

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
			sb.WriteString(`" class="fui-bar-chart__grid" data-cui-internal=""/>`)
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(axisGutter - 5))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ty + 3))
			sb.WriteString(`" class="fui-bar-chart__axis-label" data-cui-internal="" text-anchor="end">`)
			sb.WriteString(chartNumber(cfg.Ctx, tv))
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`<line x1="`)
	sb.WriteString(ftoa(axisGutter))
	sb.WriteString(`" y1="`)
	sb.WriteString(ftoa(baseY))
	sb.WriteString(`" x2="`)
	sb.WriteString(ftoa(float64(w)))
	sb.WriteString(`" y2="`)
	sb.WriteString(ftoa(baseY))
	sb.WriteString(`" class="fui-bar-chart__baseline" data-cui-internal=""/>`)

	for i, b := range cfg.Bars {
		slotX := axisGutter + float64(i)*slotW
		x := slotX + (barW-drawW)/2
		cx := x + drawW/2
		barH := (b.Value / valueMax) * plotH
		if b.Value > 0 && barH < 2 {
			barH = 2
		}
		y := baseY - barH

		if finite(b.Value) {
			barCls := "fui-bar-chart__bar"
			tone := seriesTone(b.Color, 0)
			fill := tone.fill
			if tone.palette != "" {
				barCls += " fui-bar-chart__bar--" + tone.palette
			}

			sb.WriteString(`<rect x="`)
			sb.WriteString(ftoa(x))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(y))
			sb.WriteString(`" width="`)
			sb.WriteString(ftoa(drawW))
			sb.WriteString(`" height="`)
			sb.WriteString(ftoa(barH))
			sb.WriteString(`" rx="4" class="`)
			sb.WriteString(barCls)
			sb.WriteString(`" data-cui-internal=""`)
			if fill != "" {
				sb.WriteString(` fill="`)
				sb.WriteString(escapeXML(fill))
				sb.WriteString(`"`)
			}
			if b.Label != "" {
				sb.WriteString(`><title>`)
				sb.WriteString(escapeXML(i18nui.ChartValue.Text(cfg.Ctx, map[string]any{"label": b.Label, "value": chartNumber(cfg.Ctx, b.Value)})))
				sb.WriteString(`</title></rect>`)
			} else {
				sb.WriteString(`/>`)
			}

			if showValues {
				sb.WriteString(`<text x="`)
				sb.WriteString(ftoa(cx))
				sb.WriteString(`" y="`)
				sb.WriteString(ftoa(y - 4))
				sb.WriteString(`" class="fui-bar-chart__value" data-cui-internal="" text-anchor="middle">`)
				sb.WriteString(escapeXML(chartNumber(cfg.Ctx, b.Value)))
				sb.WriteString(`</text>`)
			}
		}

		if cfg.ShowLabels && b.Label != "" {
			lines := wrapped[i]
			ly := baseY + lineH + labelGap
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ly))
			sb.WriteString(`" class="fui-bar-chart__label" data-cui-internal="" text-anchor="middle">`)
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

// barSlots divides plotW among n bars: each bar's width and the gap
// between two.
func barSlots(plotW float64, n int) (barW, gap float64) {
	const fullGap, minBar = 8.0, 3.0
	gap = fullGap
	barW = (plotW - gap*float64(n-1)) / float64(n)
	if barW >= minBar || n < 2 || plotW <= 0 {
		return max(barW, minBar), gap
	}
	barW = minBar
	gap = (plotW - minBar*float64(n)) / float64(n-1)
	if gap < 1 {
		slot := plotW / float64(n)
		barW = slot * 0.75
		gap = slot - barW
	}
	return barW, gap
}

// chartLog10 is math.Log10 that holds for subnormals too.
func chartLog10(v float64) float64 {
	if v > 0 && v < 0x1p-1022 {
		return math.Log2(v) * math.Log10(2)
	}
	return math.Log10(v)
}

// niceCeil rounds v up to 1, 1.5, 2, 2.5, 3, 4, 5, 6 or 8 times a power of ten.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Floor(chartLog10(v))
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

// niceTicks returns 0, max/2 and max.
func niceTicks(max float64) []float64 {
	return []float64{0, max / 2, max}
}

// plausibleCSSColor reports whether s is shaped like a hex color, a color
// function or var(--…); a bare word is not.
func plausibleCSSColor(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '#' {
		hex := s[1:]
		switch len(hex) {
		case 3, 4, 6, 8:
		default:
			return false
		}
		for i := 0; i < len(hex); i++ {
			c := hex[i]
			isHex := c >= '0' && c <= '9' ||
				c >= 'a' && c <= 'f' ||
				c >= 'A' && c <= 'F'
			if !isHex {
				return false
			}
		}
		return true
	}
	low := strings.ToLower(s)
	if !strings.HasSuffix(s, ")") {
		return false
	}
	if strings.HasPrefix(low, "var(--") {
		return true
	}
	for _, p := range []string{"rgb(", "rgba(", "hsl(", "hsla(", "oklch(", "color("} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// wrapChartLabel word-wraps label to at most two lines of maxChars runes,
// ellipsizing what does not fit.
func wrapChartLabel(label string, maxChars int) []string {
	if maxChars < 1 {
		maxChars = 1
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
	return `[data-cui-comp="ui-bar-chart"] {
  display: block;
  max-inline-size: 100%;
  block-size: auto;
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__bar {
  transition: opacity var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__bar:hover {
  opacity: var(--ui-bar-chart-bar-hover-opacity, 0.85);
}
.fui-bar-chart__bar--primary { fill: var(--color-primary, #18181B); }
.fui-bar-chart__bar--info    { fill: var(--color-info, #1D4ED8); }
.fui-bar-chart__bar--success { fill: var(--color-success, #166534); }
.fui-bar-chart__bar--warning { fill: var(--color-warning, #854D0E); }
.fui-bar-chart__bar--danger  { fill: var(--color-danger, #B91C1C); }

[data-cui-comp="ui-bar-chart"] .fui-bar-chart__baseline {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__grid {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
  opacity: var(--opacity-muted, 0.6);
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__value {
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  fill: var(--color-text-muted, #52525B);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__label {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-bar-chart"] .fui-bar-chart__axis-label {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-subtle, #71717A);
  font-variant-numeric: tabular-nums;
}
/* On a phone the drawing is scaled down to its card, and its text with
   it. The axis labels are drawn larger to stay readable; the gutter
   already has the room (axisPhoneScale). */
@media (max-width: 480px) {
  [data-cui-comp="ui-bar-chart"] .fui-bar-chart__axis-label {
    font-size: var(--text-base, 1rem);
  }
}`
}
