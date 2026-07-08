package ui

import (
	"math"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── PieChart / DonutChart ──────────────────────────────────────────
//
// Pure-SVG ratio chart. Slice colors come from a palette of theme
// tokens (primary, info, success, warning, danger, …); callers can
// override per slice. Donut mode is a configurable inner-radius cut.

// PieSlice is one slice of the pie.
type PieSlice struct {
	// Label is the accessible label for the slice (required when
	// LabelledBy is set on the chart — used as <title> for AT).
	Label string
	// Value is the slice value (≥0). Slices with Value=0 are skipped.
	Value float64
	// Color overrides the default palette pick. Optional CSS color
	// or one of: "primary", "info", "success", "warning", "danger".
	Color string
}

// PieChartConfig configures a PieChart.
type PieChartConfig struct {
	// Slices are the entries (≥1 with non-zero Value).
	Slices []PieSlice
	// Size is the SVG square side in CSS pixels. Default 160.
	Size int
	// InnerRadius (0–1, fraction of outer radius) cuts the center
	// out, turning it into a donut. Default 0 (pie).
	InnerRadius float64
	// CenterLabel renders a label in the middle of the donut hole.
	// Ignored when InnerRadius=0.
	CenterLabel string
	// CenterSubtext renders smaller text under the CenterLabel.
	CenterSubtext string
	// LabelledBy is the id of an element naming the chart for AT.
	// Without it the chart is aria-hidden.
	LabelledBy string
	ID         string
	Class      string
}

var pieDefaultPalette = []string{"primary", "info", "success", "warning", "danger"}

// PieChart renders a pie or donut chart.
func PieChart(cfg PieChartConfig) render.HTML {
	if len(cfg.Slices) == 0 {
		return chartEmpty(cfg.Size, cfg.LabelledBy, cfg.Class, "No data yet")
	}
	var total float64
	for _, s := range cfg.Slices {
		if s.Value < 0 {
			panic("ui: PieChart Slice.Value must be ≥0")
		}
		total += s.Value
	}
	if total == 0 {
		// Every slice is zero — nothing to draw, but a legitimate empty state.
		return chartEmpty(cfg.Size, cfg.LabelledBy, cfg.Class, "No data yet")
	}

	size := cfg.Size
	if size == 0 {
		size = 160
	}
	cx := float64(size) / 2
	cy := float64(size) / 2
	r := float64(size)/2 - 2 // 2px padding so stroke doesn't clip
	ir := cfg.InnerRadius
	if ir < 0 {
		ir = 0
	}
	if ir > 0.95 {
		ir = 0.95
	}

	cls := "ui-pie-chart"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

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
	sb.WriteString(` data-fui-comp="ui-pie-chart">`)

	// Draw slices as arc paths.
	start := -math.Pi / 2 // 12 o'clock start
	paletteIdx := 0
	for _, s := range cfg.Slices {
		if s.Value == 0 {
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
			color = pieDefaultPalette[paletteIdx%len(pieDefaultPalette)]
			paletteIdx++
		}
		cls := "ui-pie-chart__slice"
		switch color {
		case "primary", "info", "success", "warning", "danger":
			cls += " ui-pie-chart__slice--" + color
		}

		x1 := cx + r*math.Cos(start)
		y1 := cy + r*math.Sin(start)
		x2 := cx + r*math.Cos(end)
		y2 := cy + r*math.Sin(end)

		if ir > 0 {
			// Donut: trace outer arc, line in, inner arc back, line out.
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
		// If color isn't a palette key, treat as raw CSS color via
		// inline fill attribute (SVG attribute, NOT CSP-blocked inline style).
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

	// Donut center label.
	if ir > 0 && cfg.CenterLabel != "" {
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(cx))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(cy))
		sb.WriteString(`" class="ui-pie-chart__center-label" text-anchor="middle" dominant-baseline="central">`)
		sb.WriteString(escapeXML(cfg.CenterLabel))
		sb.WriteString(`</text>`)
		if cfg.CenterSubtext != "" {
			sb.WriteString(`<text x="`)
			sb.WriteString(ftoa(cx))
			sb.WriteString(`" y="`)
			sb.WriteString(ftoa(cy + 16))
			sb.WriteString(`" class="ui-pie-chart__center-sub" text-anchor="middle" dominant-baseline="central">`)
			sb.WriteString(escapeXML(cfg.CenterSubtext))
			sb.WriteString(`</text>`)
		}
	}

	sb.WriteString(`</svg>`)
	return pieChartStyle.WrapHTML(render.HTML(sb.String()))
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
	return `[data-fui-comp="ui-pie-chart"] {
  display: inline-block;
  vertical-align: middle;
}
[data-fui-comp="ui-pie-chart"] .ui-pie-chart__slice {
  stroke: var(--color-background, #FFFFFF);
  stroke-width: 1;
}
.ui-pie-chart__slice--primary { fill: var(--color-primary, #4F46E5); }
.ui-pie-chart__slice--info    { fill: var(--color-info, #3B82F6); }
.ui-pie-chart__slice--success { fill: var(--color-success, #16A34A); }
.ui-pie-chart__slice--warning { fill: var(--color-warning, #D97706); }
.ui-pie-chart__slice--danger  { fill: var(--color-danger, #DC2626); }

[data-fui-comp="ui-pie-chart"] .ui-pie-chart__center-label {
  font-size: var(--text-xl, 1.25rem);
  font-weight: 700;
  fill: var(--color-text, #18181B);
}
[data-fui-comp="ui-pie-chart"] .ui-pie-chart__center-sub {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-muted, #52525B);
}`
}
