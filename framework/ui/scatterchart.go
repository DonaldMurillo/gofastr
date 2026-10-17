package ui

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ScatterChart ───────────────────────────────────────────────────
//
// Points on two numeric axes, for the question a bar chart can't answer:
// do two measures move together (seats against MRR, latency against
// payload size)? Pure SVG, no script. Both axes are always drawn, rounded
// out to clean steps, so a point's position can be read off them. Each
// point carries a <title> naming its series, label and coordinates.

// ScatterPoint is one point.
type ScatterPoint struct {
	X, Y float64
	// Label names the point in its <title> ("Acme"). Optional.
	Label string
}

// ScatterSeries is one group of points, drawn in one color.
type ScatterSeries struct {
	// Name is the legend label and the start of each point's title.
	// Required.
	Name   string
	Points []ScatterPoint
	// Color overrides the palette pick, in the forms BarChartBar.Color
	// takes: a palette token, a registered status variant, or a CSS
	// color. Anything else falls back to the palette.
	Color string
}

// ScatterChartConfig configures a ScatterChart.
type ScatterChartConfig struct {
	// Series are the point groups. A point with a NaN or Inf coordinate
	// is a reading that never arrived and is skipped; with no finite
	// point left the chart renders the shared empty state.
	Series []ScatterSeries
	// XLabel and YLabel title the axes ("Seats", "MRR"). Optional.
	XLabel, YLabel string
	// Width / Height in CSS pixels. Default 360×220.
	Width, Height int
	// ShowLegend renders the series names under the chart, wrapping to
	// another line at the right edge; the plot gives up the height.
	ShowLegend bool
	// LabelledBy is the id of an element naming the chart for AT.
	// Without it the chart is aria-hidden, like the other charts.
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs forwards additional attributes to the root <svg>. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// data-cui-*, width, height, viewBox, xmlns, role, aria-labelledby
	// (use LabelledBy) and aria-hidden.
	ExtraAttrs html.Attrs
}

// Plot gutters: tick labels sit left of and under the plot, and an axis
// title adds a line on its side.
const (
	scatterLeft   = 36.0
	scatterRight  = 8.0
	scatterTop    = 8.0
	scatterBottom = 20.0
	scatterTitle  = 16.0
	scatterLegend = 22.0
	// scatterLegendLine is each extra legend line.
	scatterLegendLine = 16.0
	// scatterCharW is a label's width per character at --text-xs.
	scatterCharW = 7.0
)

// ScatterChart renders points on two numeric axes.
func ScatterChart(cfg ScatterChartConfig) render.HTML {
	xs, ys := []float64{}, []float64{}
	for _, s := range cfg.Series {
		if s.Name == "" {
			panic("ui: ScatterChart Series.Name required")
		}
		for _, p := range s.Points {
			if finite(p.X) && finite(p.Y) {
				xs, ys = append(xs, p.X), append(ys, p.Y)
			}
		}
	}
	if len(xs) == 0 {
		return chartEmpty(cfg.ID, cfg.LabelledBy, cfg.Class, "No data yet", cfg.ExtraAttrs)
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 {
		w = 360
	}
	if h <= 0 {
		h = 220
	}
	xTicks, xStep := axisTicks(xs)
	yTicks, yStep := axisTicks(ys)
	yText := make([]string, len(yTicks))
	widest := 0
	for i, t := range yTicks {
		yText[i] = tickText(t, yStep)
		widest = max(widest, utf8.RuneCountInString(yText[i]))
	}
	// The gutter fits the widest Y tick label.
	left, bottom := max(scatterLeft, float64(widest)*scatterCharW+10), scatterBottom
	if cfg.YLabel != "" {
		left += scatterTitle
	}
	if cfg.XLabel != "" {
		bottom += scatterTitle
	}
	// Legend entries run left to right and wrap at the right edge.
	type legendSlot struct {
		x    float64
		line int
	}
	slots := make([]legendSlot, len(cfg.Series))
	lines := 0
	if cfg.ShowLegend {
		x := left
		for i, s := range cfg.Series {
			ew := 12 + float64(utf8.RuneCountInString(s.Name))*scatterCharW
			if x > left && x+ew > float64(w)-scatterRight {
				lines, x = lines+1, left
			}
			slots[i] = legendSlot{x, lines}
			x += ew + 12
		}
		lines++
		bottom += scatterLegend + float64(lines-1)*scatterLegendLine
	}
	plotL, plotR := left, float64(w)-scatterRight
	plotT, plotB := scatterTop, float64(h)-bottom
	if plotR-plotL < 8 {
		plotR = plotL + 8
	}
	if plotB-plotT < 8 {
		plotB = plotT + 8
	}
	xLo, xHi := xTicks[0], xTicks[len(xTicks)-1]
	yLo, yHi := yTicks[0], yTicks[len(yTicks)-1]
	// Halves keep a span near the float limit (-1e308 to 1e308) finite.
	px := func(x float64) float64 { return plotL + (x/2-xLo/2)/(xHi/2-xLo/2)*(plotR-plotL) }
	py := func(y float64) float64 { return plotB - (y/2-yLo/2)/(yHi/2-yLo/2)*(plotB-plotT) }

	cls := "fui-scatter-chart"
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
	sb.WriteString(` data-cui-comp="ui-scatter-chart"`)
	sb.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs,
		"width", "height", "viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden")))
	sb.WriteString(`>`)

	line := func(x1, y1, x2, y2 float64, class string) {
		sb.WriteString(`<line x1="`)
		sb.WriteString(ftoa(x1))
		sb.WriteString(`" y1="`)
		sb.WriteString(ftoa(y1))
		sb.WriteString(`" x2="`)
		sb.WriteString(ftoa(x2))
		sb.WriteString(`" y2="`)
		sb.WriteString(ftoa(y2))
		sb.WriteString(`" class="`)
		sb.WriteString(class)
		sb.WriteString(`" data-cui-internal=""/>`)
	}
	text := func(x, y float64, anchor, class, body string) {
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(x))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(y))
		sb.WriteString(`" class="`)
		sb.WriteString(class)
		sb.WriteString(`" data-cui-internal="" text-anchor="`)
		sb.WriteString(anchor)
		sb.WriteString(`">`)
		sb.WriteString(escapeXML(body))
		sb.WriteString(`</text>`)
	}
	for i, t := range yTicks {
		y := py(t)
		line(plotL, y, plotR, y, "fui-scatter-chart__grid")
		text(plotL-5, y+3, "end", "fui-scatter-chart__tick", yText[i])
	}
	for i, t := range xTicks {
		x := px(t)
		line(x, plotT, x, plotB, "fui-scatter-chart__grid")
		anchor := "middle"
		if i == len(xTicks)-1 {
			anchor = "end"
		}
		text(x, plotB+14, anchor, "fui-scatter-chart__tick", tickText(t, xStep))
	}
	line(plotL, plotB, plotR, plotB, "fui-scatter-chart__axis")
	line(plotL, plotT, plotL, plotB, "fui-scatter-chart__axis")
	if cfg.XLabel != "" {
		text((plotL+plotR)/2, plotB+30, "middle", "fui-scatter-chart__title", cfg.XLabel)
	}
	if cfg.YLabel != "" {
		mid := (plotT + plotB) / 2
		sb.WriteString(`<text x="12" y="`)
		sb.WriteString(ftoa(mid))
		sb.WriteString(`" transform="rotate(-90 12 `)
		sb.WriteString(ftoa(mid))
		sb.WriteString(`)" class="fui-scatter-chart__title" data-cui-internal="" text-anchor="middle">`)
		sb.WriteString(escapeXML(cfg.YLabel))
		sb.WriteString(`</text>`)
	}

	tones := make([]chartTone, len(cfg.Series))
	for i, s := range cfg.Series {
		tones[i] = seriesTone(s.Color, i)
		for _, p := range s.Points {
			if !finite(p.X) || !finite(p.Y) {
				continue
			}
			sb.WriteString(`<circle cx="`)
			sb.WriteString(ftoa(px(p.X)))
			sb.WriteString(`" cy="`)
			sb.WriteString(ftoa(py(p.Y)))
			sb.WriteString(`" r="4" class="fui-scatter-chart__point`)
			tones[i].write(&sb, "fui-scatter-chart__point")
			sb.WriteString(`><title>`)
			title := s.Name + ": "
			if p.Label != "" {
				title += p.Label + " "
			}
			sb.WriteString(escapeXML(title + "(" + exactText(p.X) + ", " + exactText(p.Y) + ")"))
			sb.WriteString(`</title></circle>`)
		}
	}
	if cfg.ShowLegend {
		for i, s := range cfg.Series {
			x := slots[i].x
			y := float64(h) - 8 - float64(lines-1-slots[i].line)*scatterLegendLine
			sb.WriteString(`<circle cx="`)
			sb.WriteString(ftoa(x + 4))
			sb.WriteString(`" cy="`)
			sb.WriteString(ftoa(y - 4))
			sb.WriteString(`" r="4" class="fui-scatter-chart__point`)
			tones[i].write(&sb, "fui-scatter-chart__point")
			sb.WriteString(`/>`)
			text(x+12, y, "start", "fui-scatter-chart__legend", s.Name)
		}
	}
	sb.WriteString(`</svg>`)
	return scatterChartStyle.WrapHTML(render.HTML(sb.String()))
}

// axisTicks rounds the values' range out to clean steps (1, 2, 2.5 or 5
// times a power of ten), about four of them, and returns every tick from
// the low end to the high end with the step. A range with no width is
// widened around its value so the axis still has two ends. When no clean
// step fits in a float64 (a spread tiny beside the values, or a span past
// the float limit) the ticks are the data's two ends and the step is 0.
func axisTicks(vs []float64) ([]float64, float64) {
	lo, hi := slices.Min(vs), slices.Max(vs)
	span := hi - lo
	if span == 0 {
		span = math.Abs(hi)
		if span == 0 {
			span = 1
		}
	}
	step := niceStep(span / 4)
	kLo, kHi := math.Floor(lo/step), math.Ceil(hi/step)
	if kHi == kLo {
		kLo, kHi = kLo-1, kHi+1
	}
	const exact = 1 << 52 // integers past this are not all representable
	if math.Abs(kLo) > exact || math.Abs(kHi) > exact || !finite(kLo*step) || !finite(kHi*step) {
		return []float64{lo, hi}, 0
	}
	n := int(kHi - kLo)
	ticks := make([]float64, 0, n+1)
	for i := 0; i <= n; i++ {
		ticks = append(ticks, (kLo+float64(i))*step)
	}
	return ticks, step
}

// tickText writes a tick with as many decimals as its step needs, so no
// two ticks read the same. Very large or fallback ticks use the shortest
// exact form.
func tickText(t, step float64) string {
	if step == 0 {
		return exactText(t)
	}
	if math.Abs(t) >= 1e9 || step < 1e-9 {
		// Past plain decimals: the significant digits that tell this tick
		// from the next, so 7.5e+299 rather than 7.500000000000005e+299.
		return strconv.FormatFloat(t, 'g', decExp(t)-decExp(step)+2, 64)
	}
	d := 0
	if s := strconv.FormatFloat(step, 'f', -1, 64); strings.Contains(s, ".") {
		d = len(s) - strings.IndexByte(s, '.') - 1
	}
	p := math.Pow(10, float64(d))
	return strconv.FormatFloat(math.Round(t*p)/p, 'f', -1, 64)
}

// decExp is v's power of ten: 2 for 250, -12 for 2.5e-12.
func decExp(v float64) int {
	s := strconv.FormatFloat(v, 'e', -1, 64)
	e, _ := strconv.Atoi(s[strings.IndexByte(s, 'e')+1:])
	return e
}

// exactText is v's shortest exact form: 0.0025, 1250, 1e+21.
func exactText(v float64) string { return strconv.FormatFloat(v+0, 'g', -1, 64) }

// niceStep rounds v up to 1, 2, 2.5 or 5 times a power of ten.
func niceStep(v float64) float64 {
	base := math.Pow(10, math.Floor(math.Log10(v)))
	for _, f := range []float64{1, 2, 2.5, 5, 10} {
		if v <= f*base {
			return f * base
		}
	}
	return 10 * base
}

var scatterChartStyle = registry.RegisterStyle("ui-scatter-chart", scatterChartCSS)

func scatterChartCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-scatter-chart"] {
  display: block;
  max-inline-size: 100%;
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__grid {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
  opacity: var(--opacity-muted, 0.6);
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__axis {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__tick,
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__legend {
  font-size: var(--text-xs, 0.75rem);
  font-variant-numeric: tabular-nums;
  fill: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__title {
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  fill: var(--color-text, #09090B);
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point {
  stroke: var(--color-surface, #FFFFFF);
  stroke-width: 1;
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--primary { fill: var(--color-primary, #18181B); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--info    { fill: var(--color-info, #1D4ED8); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--success { fill: var(--color-success, #166534); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--warning { fill: var(--color-warning, #854D0E); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--danger  { fill: var(--color-danger, #B91C1C); }`
}
