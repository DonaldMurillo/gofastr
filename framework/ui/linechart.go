package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── LineChart ──────────────────────────────────────────────────────
//
// Multi-series time-series SVG line chart. Pure render, no JS. Each
// series is a {Name, Values} pair; values share an x-axis indexed
// by position (i.e. uniform sampling). Colors cycle through the
// theme palette.

// LineSeries is one series.
type LineSeries struct {
	// Name is the legend label (required).
	Name string
	// Values are the y-values in order.
	Values []float64
	// Color overrides the default palette pick. Optional palette key
	// or raw CSS color.
	Color string
	// Area, when true, fills under the line.
	Area bool
}

// LineChartConfig configures a LineChart.
type LineChartConfig struct {
	// Series are the lines (≥1, each with ≥2 Values).
	Series []LineSeries
	// Labels are optional x-axis tick labels. When non-empty, must
	// match the length of the longest series.
	Labels []string
	// Width / Height in CSS pixels. Default 360×200.
	Width  int
	Height int
	// ShowLegend renders a small legend strip below the chart.
	ShowLegend bool
	// LabelledBy is the id of an element naming the chart for AT.
	LabelledBy string
	ID         string
	Class      string
}

// LineChart renders a multi-series line chart.
func LineChart(cfg LineChartConfig) render.HTML {
	if len(cfg.Series) == 0 {
		panic("ui: LineChart requires ≥1 Series")
	}
	for _, s := range cfg.Series {
		if s.Name == "" {
			panic("ui: LineChart Series.Name required")
		}
		if len(s.Values) < 2 {
			panic("ui: LineChart Series.Values must have ≥2 points")
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
	legendGutter := 0
	if cfg.ShowLegend {
		legendGutter = 22
	}
	plotW := float64(w)
	plotH := float64(h - bottomGutter - legendGutter)

	// Global min/max.
	min := cfg.Series[0].Values[0]
	max := min
	for _, s := range cfg.Series {
		for _, v := range s.Values {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}

	cls := "ui-line-chart"
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
	sb.WriteString(` data-fui-comp="ui-line-chart">`)

	palette := []string{"primary", "info", "success", "warning", "danger"}
	for i, s := range cfg.Series {
		color := s.Color
		isPalette := color == "primary" || color == "info" ||
			color == "success" || color == "warning" || color == "danger"
		if color == "" {
			color = palette[i%len(palette)]
			isPalette = true
		}

		n := len(s.Values)
		pts := make([]string, 0, n)
		for j, v := range s.Values {
			x := float64(j) * plotW / float64(n-1)
			y := plotH - ((v - min) / span * plotH)
			pts = append(pts, ftoa(x)+","+ftoa(y))
		}
		pathD := "M" + pts[0]
		for j := 1; j < len(pts); j++ {
			pathD += " L" + pts[j]
		}

		seriesCls := "ui-line-chart__line"
		if isPalette {
			seriesCls += " ui-line-chart__line--" + color
		}

		if s.Area {
			areaD := pathD + " L" + ftoa(plotW) + "," + ftoa(plotH) +
				" L0," + ftoa(plotH) + " Z"
			areaCls := "ui-line-chart__area"
			if isPalette {
				areaCls += " ui-line-chart__area--" + color
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
		sb.WriteString(pathD)
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

	// X-axis labels.
	if len(cfg.Labels) > 0 {
		ny := plotH + 12
		// Use the longest series length to map labels to positions.
		var longest int
		for _, s := range cfg.Series {
			if len(s.Values) > longest {
				longest = len(s.Values)
			}
		}
		// Just render up to len(Labels) evenly across plotW.
		nl := len(cfg.Labels)
		for i, lbl := range cfg.Labels {
			x := float64(i) * plotW / float64(nl-1)
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(x))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(ny))
			sb.WriteString(`" class="ui-line-chart__label" text-anchor="middle">`)
			sb.WriteString(escapeXML(lbl))
			sb.WriteString(`</text>`)
		}
	}

	// Legend.
	if cfg.ShowLegend {
		legY := plotH + float64(bottomGutter) + 14
		x := 0.0
		for i, s := range cfg.Series {
			color := s.Color
			isPalette := color == "primary" || color == "info" ||
				color == "success" || color == "warning" || color == "danger"
			if color == "" {
				color = palette[i%len(palette)]
				isPalette = true
			}
			swatchCls := "ui-line-chart__legend-swatch"
			if isPalette {
				swatchCls += " ui-line-chart__legend-swatch--" + color
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
			sb.WriteString(`" class="ui-line-chart__legend">`)
			sb.WriteString(escapeXML(s.Name))
			sb.WriteString(`</text>`)
			x += float64(len(s.Name))*7 + 24
		}
	}

	sb.WriteString(`</svg>`)
	return lineChartStyle.WrapHTML(render.HTML(sb.String()))
}

var lineChartStyle = registry.RegisterStyle("ui-line-chart", lineChartCSS)

func lineChartCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-line-chart"] {
  display: block;
  max-inline-size: 100%;
}
[data-fui-comp="ui-line-chart"] .ui-line-chart__line {
  fill: none;
  stroke-width: 1.5;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.ui-line-chart__line--primary { stroke: var(--color-primary, #4F46E5); }
.ui-line-chart__line--info    { stroke: var(--color-info, #3B82F6); }
.ui-line-chart__line--success { stroke: var(--color-success, #16A34A); }
.ui-line-chart__line--warning { stroke: var(--color-warning, #D97706); }
.ui-line-chart__line--danger  { stroke: var(--color-danger, #DC2626); }

[data-fui-comp="ui-line-chart"] .ui-line-chart__area { opacity: 0.18; stroke: none; }
.ui-line-chart__area--primary { fill: var(--color-primary, #4F46E5); }
.ui-line-chart__area--info    { fill: var(--color-info, #3B82F6); }
.ui-line-chart__area--success { fill: var(--color-success, #16A34A); }
.ui-line-chart__area--warning { fill: var(--color-warning, #D97706); }
.ui-line-chart__area--danger  { fill: var(--color-danger, #DC2626); }

[data-fui-comp="ui-line-chart"] .ui-line-chart__label {
  font-size: 0.7rem;
  fill: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-line-chart"] .ui-line-chart__legend {
  font-size: 0.75rem;
  fill: var(--color-text, #18181B);
  font-weight: 500;
}
.ui-line-chart__legend-swatch--primary { fill: var(--color-primary, #4F46E5); }
.ui-line-chart__legend-swatch--info    { fill: var(--color-info, #3B82F6); }
.ui-line-chart__legend-swatch--success { fill: var(--color-success, #16A34A); }
.ui-line-chart__legend-swatch--warning { fill: var(--color-warning, #D97706); }
.ui-line-chart__legend-swatch--danger  { fill: var(--color-danger, #DC2626); }`
}
