package ui

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PieSlice is one slice of the pie.
type PieSlice struct {
	// Label is the slice's <title>.
	Label string
	// Value is the slice's size, at least 0; a zero slice is skipped.
	Value float64
	// Color is a palette token or a raw CSS color; empty cycles the palette.
	Color string
}

// PieChartConfig configures a PieChart.
type PieChartConfig struct {
	Slices []PieSlice
	// Size is the square's side in CSS pixels. Default 160.
	Size int
	// InnerRadius, a fraction of the radius up to 0.95, cuts a donut hole.
	InnerRadius float64
	// CenterLabel and CenterSubtext are drawn in a donut's hole.
	CenterLabel   string
	CenterSubtext string
	// LabelledBy names the chart; without it the chart is aria-hidden.
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs go on the root <svg>, less the keys the chart derives.
	ExtraAttrs html.Attrs
	// Ctx sets the locale of numbers and words; nil is English.
	Ctx context.Context
}

// PieChart renders a pie or donut chart.
func PieChart(cfg PieChartConfig) render.HTML {
	if len(cfg.Slices) == 0 {
		return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, "", cfg.ExtraAttrs)
	}
	var total float64
	for _, s := range cfg.Slices {
		if !finite(s.Value) {
			continue
		}
		if s.Value < 0 {
			panic("ui: PieChart Slice.Value must be ≥0")
		}
		total += s.Value
	}
	if total == 0 || !finite(total) {
		return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, "", cfg.ExtraAttrs)
	}

	size := cfg.Size
	if size == 0 {
		size = 160
	}
	cx := float64(size) / 2
	cy := float64(size) / 2
	r := float64(size)/2 - 2
	ir := cfg.InnerRadius
	if ir < 0 {
		ir = 0
	}
	if ir > 0.95 {
		ir = 0.95
	}

	cls := "fui-pie-chart"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	extra := html.SafeExtraAttrs(cfg.ExtraAttrs,
		"width", "height", "viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden")

	var sb strings.Builder
	sb.WriteString(`<svg width="`)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(`" height="`)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(`" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(size))
	sb.WriteString(` `)
	sb.WriteString(strconv.Itoa(size))
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
	sb.WriteString(` data-cui-comp="ui-pie-chart"`)
	sb.WriteString(serializeExtraAttrs(extra))
	sb.WriteString(`>`)

	start := -math.Pi / 2
	paletteIdx := 0
	for _, s := range cfg.Slices {
		if s.Value == 0 || !finite(s.Value) {
			continue
		}
		frac := s.Value / total
		end := start + frac*2*math.Pi
		large := 0
		if frac > 0.5 {
			large = 1
		}

		color := s.Color
		if color == "" {
			color = chartPalette[paletteIdx%len(chartPalette)]
			paletteIdx++
		}
		cls := "fui-pie-chart__slice"
		switch color {
		case "primary", "info", "success", "warning", "danger":
			cls += " fui-pie-chart__slice--" + color
		}

		x1 := cx + r*math.Cos(start)
		y1 := cy + r*math.Sin(start)
		x2 := cx + r*math.Cos(end)
		y2 := cy + r*math.Sin(end)

		if frac > 0.5 && ftoa(x1) == ftoa(x2) && ftoa(y1) == ftoa(y2) {
			sb.WriteString(`<path d="`)
			sb.WriteString(pieFullCircle(cx, cy, r, 1))
			if ir > 0 {
				sb.WriteString(` `)
				sb.WriteString(pieFullCircle(cx, cy, r*ir, 0))
			}
			sb.WriteString(`" class="`)
			sb.WriteString(cls)
			sb.WriteString(`"`)
		} else if ir > 0 {
			rin := r * ir
			ix1 := cx + rin*math.Cos(start)
			iy1 := cy + rin*math.Sin(start)
			ix2 := cx + rin*math.Cos(end)
			iy2 := cy + rin*math.Sin(end)
			sb.WriteString(`<path d="M`)
			sb.WriteString(ftoa(x1))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(y1))
			sb.WriteString(` A`)
			sb.WriteString(ftoa(r))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(r))
			sb.WriteString(` 0 `)
			sb.WriteString(strconv.Itoa(large))
			sb.WriteString(` 1 `)
			sb.WriteString(ftoa(x2))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(y2))
			sb.WriteString(` L`)
			sb.WriteString(ftoa(ix2))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(iy2))
			sb.WriteString(` A`)
			sb.WriteString(ftoa(rin))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(rin))
			sb.WriteString(` 0 `)
			sb.WriteString(strconv.Itoa(large))
			sb.WriteString(` 0 `)
			sb.WriteString(ftoa(ix1))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(iy1))
			sb.WriteString(` Z" class="`)
			sb.WriteString(cls)
			sb.WriteString(`"`)
		} else {
			sb.WriteString(`<path d="M`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(cy))
			sb.WriteString(` L`)
			sb.WriteString(ftoa(x1))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(y1))
			sb.WriteString(` A`)
			sb.WriteString(ftoa(r))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(r))
			sb.WriteString(` 0 `)
			sb.WriteString(strconv.Itoa(large))
			sb.WriteString(` 1 `)
			sb.WriteString(ftoa(x2))
			sb.WriteString(`,`)
			sb.WriteString(ftoa(y2))
			sb.WriteString(` Z" class="`)
			sb.WriteString(cls)
			sb.WriteString(`"`)
		}
		isPalette := color == "primary" || color == "info" ||
			color == "success" || color == "warning" || color == "danger"
		if !isPalette {
			sb.WriteString(` fill="`)
			sb.WriteString(escapeXML(color))
			sb.WriteString(`"`)
		}
		if s.Label != "" {
			sb.WriteString(`><title>`)
			sb.WriteString(escapeXML(s.Label))
			sb.WriteString(`</title></path>`)
		} else {
			sb.WriteString(`/>`)
		}

		start = end
	}

	if ir > 0 && cfg.CenterLabel != "" {
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(cx))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(cy))
		sb.WriteString(`" class="fui-pie-chart__center-label" text-anchor="middle" dominant-baseline="central">`)
		sb.WriteString(escapeXML(cfg.CenterLabel))
		sb.WriteString(`</text>`)
		if cfg.CenterSubtext != "" {
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(cy + 16))
			sb.WriteString(`" class="fui-pie-chart__center-sub" text-anchor="middle" dominant-baseline="central">`)
			sb.WriteString(escapeXML(cfg.CenterSubtext))
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`</svg>`)
	return pieChartStyle.WrapHTML(render.HTML(sb.String()))
}

// pieFullCircle is a circle as two half arcs from 12 o'clock, clockwise
// for sweep 1.
func pieFullCircle(cx, cy, r float64, sweep int) string {
	arc := " A" + ftoa(r) + "," + ftoa(r) + " 0 1 " + strconv.Itoa(sweep) + " "
	top := ftoa(cx) + "," + ftoa(cy-r)
	return "M" + top + arc + ftoa(cx) + "," + ftoa(cy+r) + arc + top + " Z"
}

func escapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}

var pieChartStyle = registry.RegisterStyle("ui-pie-chart", pieChartCSS)

func pieChartCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-pie-chart"] {
  display: inline-block;
  vertical-align: middle;
}
[data-cui-comp="ui-pie-chart"] .fui-pie-chart__slice {
  stroke: var(--color-background, #FFFFFF);
  stroke-width: 1;
}
.fui-pie-chart__slice--primary { fill: var(--color-primary, #18181B); }
.fui-pie-chart__slice--info    { fill: var(--color-info, #1D4ED8); }
.fui-pie-chart__slice--success { fill: var(--color-success, #166534); }
.fui-pie-chart__slice--warning { fill: var(--color-warning, #854D0E); }
.fui-pie-chart__slice--danger  { fill: var(--color-danger, #B91C1C); }

[data-cui-comp="ui-pie-chart"] .fui-pie-chart__center-label {
  font-size: var(--text-xl, 1.25rem);
  font-weight: var(--font-weight-bold);
  fill: var(--color-text, #09090B);
}
[data-cui-comp="ui-pie-chart"] .fui-pie-chart__center-sub {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-muted, #52525B);
}`
}
