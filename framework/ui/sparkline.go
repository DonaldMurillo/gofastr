package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Sparkline ──────────────────────────────────────────────────────
//
// Inline SVG trend renderer. Pure render — no JS, no hydration. Takes
// a series of float64 values, normalizes to a unit box, draws as a
// path. Two shapes: line (default) and area.
//
// Pairs with StatCard — use as a Slot under the metric value.

// SparklineShape picks line (default) or area.
type SparklineShape string

const (
	SparklineLine SparklineShape = ""
	SparklineArea SparklineShape = "area"
)

// SparklineConfig configures a Sparkline.
type SparklineConfig struct {
	// Values are the points in order (≥2).
	Values []float64
	// Width / Height in CSS pixels. Default 120×32.
	Width  int
	Height int
	// Shape picks line or area. Default line.
	Shape SparklineShape
	// Color override — defaults to var(--color-primary) via CSS.
	// Set to "danger" / "success" / "warning" / "info" to use the
	// matching theme token; any other string is passed through as a
	// raw CSS color.
	Color string
	// LabelledBy is the id of an element naming the chart (e.g. the
	// StatCard label) — used as the SVG's aria-labelledby. Without
	// it the chart is aria-hidden (decorative).
	LabelledBy string
	ID         string
	Class      string
	// Ctx carries the per-request context used to resolve the no-trend-data label.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// Sparkline renders a tiny inline trend chart.
func Sparkline(cfg SparklineConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if len(cfg.Values) < 2 {
		// Inline trend with too few points: render a calm "no trend" dash
		// rather than crashing the host that embeds it.
		attrs := map[string]string{"data-fui-comp": "ui-sparkline", "aria-label": i18nui.T(ctx, i18nui.KeySparklineNoData)}
		if cfg.Class != "" {
			attrs["class"] = cfg.Class
		}
		return render.Tag("span", attrs, render.Text("—"))
	}
	switch cfg.Shape {
	case SparklineLine, SparklineArea:
	default:
		panic("ui: Sparkline unknown Shape " + string(cfg.Shape) +
			` — pick one of: "" (line), area`)
	}
	w := cfg.Width
	if w == 0 {
		w = 120
	}
	h := cfg.Height
	if h == 0 {
		h = 32
	}

	min, max := cfg.Values[0], cfg.Values[0]
	for _, v := range cfg.Values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}

	// Map to viewBox coords. Leave a 1px margin top + bottom so the
	// line stroke doesn't get clipped.
	pad := 1.0
	plotW := float64(w)
	plotH := float64(h) - 2*pad
	n := len(cfg.Values)
	pts := make([]string, 0, n)
	for i, v := range cfg.Values {
		x := float64(i) * plotW / float64(n-1)
		y := pad + plotH - ((v - min) / span * plotH)
		pts = append(pts, ftoa(x)+","+ftoa(y))
	}

	cls := "ui-sparkline"
	if cfg.Color != "" {
		cls += " ui-sparkline--" + escapeXML(cfg.Color)
	}
	if cfg.Class != "" {
		cls += " " + escapeXML(cfg.Class)
	}

	svgAttrs := strings.Builder{}
	svgAttrs.WriteString(`width="`)
	svgAttrs.WriteString(strconv.Itoa(w))
	svgAttrs.WriteString(`" height="`)
	svgAttrs.WriteString(strconv.Itoa(h))
	svgAttrs.WriteString(`" viewBox="0 0 `)
	svgAttrs.WriteString(strconv.Itoa(w))
	svgAttrs.WriteString(` `)
	svgAttrs.WriteString(strconv.Itoa(h))
	svgAttrs.WriteString(`" class="`)
	svgAttrs.WriteString(cls)
	svgAttrs.WriteString(`" xmlns="http://www.w3.org/2000/svg"`)
	if cfg.ID != "" {
		svgAttrs.WriteString(` id="`)
		svgAttrs.WriteString(escapeXML(cfg.ID))
		svgAttrs.WriteString(`"`)
	}
	if cfg.LabelledBy != "" {
		svgAttrs.WriteString(` role="img" aria-labelledby="`)
		svgAttrs.WriteString(escapeXML(cfg.LabelledBy))
		svgAttrs.WriteString(`"`)
	} else {
		svgAttrs.WriteString(` aria-hidden="true"`)
	}

	// Build the path. For area, append baseline-close so the fill
	// looks like a hill silhouette; for line, just the polyline.
	pathD := "M" + pts[0]
	for i := 1; i < len(pts); i++ {
		pathD += " L" + pts[i]
	}
	areaD := pathD + " L" + ftoa(plotW) + "," + ftoa(float64(h)) +
		" L0," + ftoa(float64(h)) + " Z"

	var body string
	if cfg.Shape == SparklineArea {
		body = `<path d="` + areaD + `" class="ui-sparkline__area"/><path d="` +
			pathD + `" class="ui-sparkline__line"/>`
	} else {
		body = `<path d="` + pathD + `" class="ui-sparkline__line"/>`
	}

	out := `<svg ` + svgAttrs.String() + ` data-fui-comp="ui-sparkline">` + body + `</svg>`
	return sparklineStyle.WrapHTML(render.HTML(out))
}

func ftoa(f float64) string {
	// Two decimals, trimmed of trailing zeros. Keeps SVG path strings
	// small without losing visible precision.
	s := strconv.FormatFloat(f, 'f', 2, 64)
	for strings.HasSuffix(s, "0") {
		s = s[:len(s)-1]
	}
	if strings.HasSuffix(s, ".") {
		s = s[:len(s)-1]
	}
	return s
}

var sparklineStyle = registry.RegisterStyle("ui-sparkline", sparklineCSS)

func sparklineCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-sparkline"] {
  display: inline-block;
  vertical-align: middle;
  color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-sparkline"] .ui-sparkline__line {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linejoin: round;
  stroke-linecap: round;
}
[data-fui-comp="ui-sparkline"] .ui-sparkline__area {
  fill: currentColor;
  opacity: 0.18;
  stroke: none;
}

/* Color presets — recolor via currentColor on the SVG root. */
.ui-sparkline.ui-sparkline--success { color: var(--color-success, #16A34A); }
.ui-sparkline.ui-sparkline--warning { color: var(--color-warning, #D97706); }
.ui-sparkline.ui-sparkline--danger  { color: var(--color-danger, #DC2626); }
.ui-sparkline.ui-sparkline--info    { color: var(--color-info, #3B82F6); }`
}
