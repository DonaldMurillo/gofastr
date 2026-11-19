package ui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// LineSeries is one series.
type LineSeries struct {
	// Name is the legend label. Required.
	Name   string
	Values []float64
	// Color is a palette token or a raw CSS color; empty cycles the palette.
	Color string
	// Area fills under the line.
	Area bool
}

// LineChartConfig configures a LineChart.
type LineChartConfig struct {
	// Series are the lines, each with at least two finite Values; value
	// i sits at the same x in every series.
	Series []LineSeries
	// Labels are optional x-axis labels, label i over point i.
	Labels []string
	// Width and Height in CSS pixels. Default 360×200.
	Width      int
	Height     int
	ShowLegend bool
	// LabelledBy names the chart; without it the chart is aria-hidden.
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs go on the root <svg>, less the keys the chart derives.
	ExtraAttrs html.Attrs
	// Ctx sets the locale of numbers and words; nil is English.
	Ctx context.Context
}

// LineChart renders a multi-series line chart.
func LineChart(cfg LineChartConfig) render.HTML {
	if len(cfg.Series) == 0 {
		return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, "", cfg.ExtraAttrs)
	}
	for _, s := range cfg.Series {
		if s.Name == "" {
			panic("ui: LineChart Series.Name required")
		}
		if _, _, n := finiteRange(s.Values); n < 2 {
			return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, i18nui.ChartNotEnoughData.Text(cfg.Ctx), cfg.ExtraAttrs)
		}
	}
	w := cfg.Width
	if w == 0 {
		w = 360
	}
	h := cfg.Height
	if h == 0 {
		h = 200
	}
	bottomGutter := 0
	if len(cfg.Labels) > 0 {
		bottomGutter = 16
	}
	type legendSlot struct {
		x    float64
		line int
	}
	legendSlots := make([]legendSlot, len(cfg.Series))
	legendGutter := 0
	if cfg.ShowLegend {
		x, line := 0.0, 0
		for index, series := range cfg.Series {
			width := 12 + float64(utf8.RuneCountInString(series.Name))*7
			if x > 0 && x+width > float64(w) {
				x, line = 0, line+1
			}
			legendSlots[index] = legendSlot{x: x, line: line}
			x += width + 12
		}
		legendGutter = (line + 1) * 22
	}
	h = max(h, bottomGutter+legendGutter+8)
	plotW := float64(w)
	plotH := float64(h - bottomGutter - legendGutter)

	var min, max float64
	for i, s := range cfg.Series {
		lo, hi, _ := finiteRange(s.Values)
		if i == 0 || lo < min {
			min = lo
		}
		if i == 0 || hi > max {
			max = hi
		}
	}

	longest := 0
	for _, s := range cfg.Series {
		if len(s.Values) > longest {
			longest = len(s.Values)
		}
	}

	cls := "fui-line-chart"
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
	sb.WriteString(` data-cui-comp="ui-line-chart"`)
	sb.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs, "width", "height",
		"viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden")))
	sb.WriteString(`>`)

	const linePad = 4.0
	inner := plotH - 2*linePad
	for _, gy := range []float64{linePad, linePad + inner/2, plotH - linePad} {
		sb.WriteString(`<line x1="0" y1="`)
		sb.WriteString(ftoa(gy))
		sb.WriteString(`" x2="`)
		sb.WriteString(ftoa(plotW))
		sb.WriteString(`" y2="`)
		sb.WriteString(ftoa(gy))
		sb.WriteString(`" class="fui-line-chart__grid" data-cui-internal=""/>`)
	}

	for i, s := range cfg.Series {
		color := s.Color
		isPalette := slices.Contains(chartPalette, color)
		if color == "" {
			color = chartPalette[i%len(chartPalette)]
			isPalette = true
		}

		pts, firstX, lastX := chartPoints(s.Values, longest-1, plotW, linePad, inner, min, max)
		var pathD strings.Builder
		pathD.WriteString("M" + pts[0])
		for j := 1; j < len(pts); j++ {
			pathD.WriteString(" L" + pts[j])
		}

		seriesCls := "fui-line-chart__line"
		if isPalette {
			seriesCls += " fui-line-chart__line--" + color
		}

		if s.Area {
			areaD := pathD.String() + " L" + ftoa(lastX) + "," + ftoa(plotH) +
				" L" + ftoa(firstX) + "," + ftoa(plotH) + " Z"
			areaCls := "fui-line-chart__area"
			if isPalette {
				areaCls += " fui-line-chart__area--" + color
			}
			sb.WriteString(`<path d="`)
			sb.WriteString(areaD)
			sb.WriteString(`" class="`)
			sb.WriteString(areaCls)
			sb.WriteString(`"`)
			if !isPalette {
				sb.WriteString(` fill="`)
				sb.WriteString(escapeXML(color))
				sb.WriteString(`"`)
			}
			sb.WriteString(`/>`)
		}

		sb.WriteString(`<path d="`)
		sb.WriteString(pathD.String())
		sb.WriteString(`" class="`)
		sb.WriteString(seriesCls)
		sb.WriteString(`"`)
		if !isPalette {
			sb.WriteString(` stroke="`)
			sb.WriteString(escapeXML(color))
			sb.WriteString(`"`)
		}
		sb.WriteString(`><title>`)
		sb.WriteString(escapeXML(s.Name))
		sb.WriteString(`</title></path>`)
	}

	if len(cfg.Labels) > 0 {
		ny := plotH + 12
		for i, lbl := range cfg.Labels {
			if i >= longest {
				break
			}
			x := float64(i) * plotW / float64(longest-1)
			anchor := "middle"
			switch i {
			case 0:
				anchor = "start"
			case longest - 1:
				anchor = "end"
			}
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(x))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ny))
			sb.WriteString(`" class="fui-line-chart__label" text-anchor="`)
			sb.WriteString(anchor)
			sb.WriteString(`">`)
			sb.WriteString(escapeXML(lbl))
			sb.WriteString(`</text>`)
		}
	}

	if cfg.ShowLegend {
		for i, s := range cfg.Series {
			x := legendSlots[i].x
			legY := plotH + float64(bottomGutter) + 14 + float64(legendSlots[i].line)*22
			color := s.Color
			isPalette := slices.Contains(chartPalette, color)
			if color == "" {
				color = chartPalette[i%len(chartPalette)]
				isPalette = true
			}
			swatchCls := "fui-line-chart__legend-swatch"
			if isPalette {
				swatchCls += " fui-line-chart__legend-swatch--" + color
			}
			sb.WriteString(`<circle cx="`)
			sb.WriteString(ftoa(x + 4))
			sb.WriteString(`" cy="`)
			sb.WriteString(ftoa(legY))
			sb.WriteString(`" r="4" class="`)
			sb.WriteString(swatchCls)
			sb.WriteString(`"`)
			if !isPalette {
				sb.WriteString(` fill="`)
				sb.WriteString(escapeXML(color))
				sb.WriteString(`"`)
			}
			sb.WriteString(`/>`)
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(x + 12))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(legY + 4))
			sb.WriteString(`" class="fui-line-chart__legend">`)
			sb.WriteString(escapeXML(s.Name))
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`</svg>`)
	return lineChartStyle.WrapHTML(render.HTML(sb.String()))
}

var lineChartStyle = registry.RegisterStyle("ui-line-chart", lineChartCSS)

func lineChartCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-line-chart"] {
  display: block;
  max-inline-size: 100%;
}
[data-cui-comp="ui-line-chart"] .fui-line-chart__line {
  fill: none;
  stroke-width: 2;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.fui-line-chart__line--primary { stroke: var(--color-primary, #18181B); }
.fui-line-chart__line--info    { stroke: var(--color-info, #1D4ED8); }
.fui-line-chart__line--success { stroke: var(--color-success, #166534); }
.fui-line-chart__line--warning { stroke: var(--color-warning, #854D0E); }
.fui-line-chart__line--danger  { stroke: var(--color-danger, #B91C1C); }

[data-cui-comp="ui-line-chart"] .fui-line-chart__grid {
  stroke: var(--color-border, #E4E4E7);
  stroke-width: 1;
  opacity: var(--opacity-muted, 0.6);
}
[data-cui-comp="ui-line-chart"] .fui-line-chart__area { opacity: var(--opacity-faint, 0.2); stroke: none; }
.fui-line-chart__area--primary { fill: var(--color-primary, #18181B); }
.fui-line-chart__area--info    { fill: var(--color-info, #1D4ED8); }
.fui-line-chart__area--success { fill: var(--color-success, #166534); }
.fui-line-chart__area--warning { fill: var(--color-warning, #854D0E); }
.fui-line-chart__area--danger  { fill: var(--color-danger, #B91C1C); }

[data-cui-comp="ui-line-chart"] .fui-line-chart__label {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-line-chart"] .fui-line-chart__legend {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text, #09090B);
  font-weight: var(--font-weight-medium);
}
.fui-line-chart__legend-swatch--primary { fill: var(--color-primary, #18181B); }
.fui-line-chart__legend-swatch--info    { fill: var(--color-info, #1D4ED8); }
.fui-line-chart__legend-swatch--success { fill: var(--color-success, #166534); }
.fui-line-chart__legend-swatch--warning { fill: var(--color-warning, #854D0E); }
.fui-line-chart__legend-swatch--danger  { fill: var(--color-danger, #B91C1C); }`
}
