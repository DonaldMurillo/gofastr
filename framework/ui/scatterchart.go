package ui

import (
	"math"
	"slices"
	"strconv"
	"strings"

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
	// ShowLegend renders the series names under the chart.
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
	left, bottom := scatterLeft, scatterBottom
	if cfg.YLabel != "" {
		left += scatterTitle
	}
	if cfg.XLabel != "" {
		bottom += scatterTitle
	}
	if cfg.ShowLegend {
		bottom += scatterLegend
	}
	plotL, plotR := left, float64(w)-scatterRight
	plotT, plotB := scatterTop, float64(h)-bottom
	if plotR-plotL < 8 {
		plotR = plotL + 8
	}
	if plotB-plotT < 8 {
		plotB = plotT + 8
	}
	xTicks, yTicks := axisTicks(xs), axisTicks(ys)
	xLo, xHi := xTicks[0], xTicks[len(xTicks)-1]
	yLo, yHi := yTicks[0], yTicks[len(yTicks)-1]
	px := func(x float64) float64 { return plotL + (x-xLo)/(xHi-xLo)*(plotR-plotL) }
	py := func(y float64) float64 { return plotB - (y-yLo)/(yHi-yLo)*(plotB-plotT) }

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
	for _, t := range yTicks {
		y := py(t)
		line(plotL, y, plotR, y, "fui-scatter-chart__grid")
		text(plotL-5, y+3, "end", "fui-scatter-chart__tick", ftoa(t))
	}
	for i, t := range xTicks {
		x := px(t)
		line(x, plotT, x, plotB, "fui-scatter-chart__grid")
		anchor := "middle"
		if i == len(xTicks)-1 {
			anchor = "end"
		}
		text(x, plotB+14, anchor, "fui-scatter-chart__tick", ftoa(t))
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
			sb.WriteString(escapeXML(title + "(" + ftoa(p.X) + ", " + ftoa(p.Y) + ")"))
			sb.WriteString(`</title></circle>`)
		}
	}
	if cfg.ShowLegend {
		y := float64(h) - 8
		x := plotL
		for i, s := range cfg.Series {
			sb.WriteString(`<circle cx="`)
			sb.WriteString(ftoa(x + 4))
			sb.WriteString(`" cy="`)
			sb.WriteString(ftoa(y - 4))
			sb.WriteString(`" r="4" class="fui-scatter-chart__point`)
			tones[i].write(&sb, "fui-scatter-chart__point")
			sb.WriteString(`/>`)
			text(x+12, y, "start", "fui-scatter-chart__legend", s.Name)
			x += float64(len(s.Name))*7 + 24
		}
	}
	sb.WriteString(`</svg>`)
	return scatterChartStyle.WrapHTML(render.HTML(sb.String()))
}

// chartTone is a mark's resolved color: a palette class or a raw fill.
type chartTone struct{ palette, fill string }

// seriesTone resolves a series Color the way BarChart resolves a bar's:
// palette token, registered status variant, plausible CSS color, else
// the i-th palette color.
func seriesTone(color string, i int) chartTone {
	switch {
	case slices.Contains(chartPalette, color):
		return chartTone{palette: color}
	case color == "":
	default:
		if c, ok := registeredStatusColor(color); ok {
			return chartTone{fill: c}
		}
		if plausibleCSSColor(color) {
			return chartTone{fill: color}
		}
	}
	return chartTone{palette: chartPalette[i%len(chartPalette)]}
}

// write closes the class attribute with the palette modifier, or with a
// fill attribute after it, and leaves the element open.
func (t chartTone) write(sb *strings.Builder, base string) {
	if t.palette != "" {
		sb.WriteString(` ` + base + `--` + t.palette + `" data-cui-internal=""`)
		return
	}
	sb.WriteString(`" data-cui-internal="" fill="`)
	sb.WriteString(escapeXML(t.fill))
	sb.WriteString(`"`)
}

var chartPalette = []string{"primary", "info", "success", "warning", "danger"}

// axisTicks rounds the values' range out to clean steps (1, 2, 2.5 or 5
// times a power of ten), about four of them, and returns every tick from
// the low end to the high end. A range with no width is widened around
// its value so the axis still has two ends.
func axisTicks(vs []float64) []float64 {
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
	ticks := make([]float64, 0, int(kHi-kLo)+1)
	for k := kLo; k <= kHi; k++ {
		ticks = append(ticks, k*step)
	}
	return ticks
}

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
  fill: var(--color-text, #18181B);
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point {
  stroke: var(--color-surface, #FFFFFF);
  stroke-width: 1;
}
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--primary { fill: var(--color-primary, #18181B); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--info    { fill: var(--color-info, #3B82F6); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--success { fill: var(--color-success, #16A34A); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--warning { fill: var(--color-warning, #D97706); }
[data-cui-comp="ui-scatter-chart"] .fui-scatter-chart__point--danger  { fill: var(--color-danger, #DC2626); }`
}
