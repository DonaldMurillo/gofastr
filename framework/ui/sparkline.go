package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Sparkline ──────────────────────────────────────────────────────
//
// Inline SVG trend renderer. Pure render, no JS, no hydration. Takes
// a series of float64 values, normalizes to a unit box, draws as a
// path. Two shapes: line (default) and area.
//
// Pairs with StatCard. Use as a Slot under the metric value.

// SparklineShape picks line (default) or area.
type SparklineShape string

const (
	SparklineLine SparklineShape = ""
	SparklineArea SparklineShape = "area"
)

// SparklineColor picks the theme token the line is drawn in. A new
// color is a theme token, never a raw CSS value set here.
type SparklineColor string

const (
	SparklinePrimary SparklineColor = "" // var(--color-primary)
	SparklineSuccess SparklineColor = "success"
	SparklineWarning SparklineColor = "warning"
	SparklineDanger  SparklineColor = "danger"
	SparklineInfo    SparklineColor = "info"
)

// SparklineConfig configures a Sparkline.
type SparklineConfig struct {
	// Values are the points in order (≥2 finite). A NaN or Inf value is
	// a reading that never arrived: the line joins the points either
	// side of it.
	Values []float64
	// Width / Height in CSS pixels. Default 120×32.
	Width  int
	Height int
	// FullWidth stretches the chart to its container's content width
	// (width="100%"): the viewBox keeps the configured aspect, Height
	// stays fixed. For cards whose column width is fluid (responsive
	// grids) where a fixed px width would leave dead margins.
	FullWidth bool
	// Shape picks line or area. Default line.
	Shape SparklineShape
	// Color picks the line's theme token. Default SparklinePrimary;
	// any value outside the SparklineColor constants panics.
	Color SparklineColor
	// LabelledBy is the id of an element naming the chart (e.g. the
	// StatCard label), used as the SVG's aria-labelledby. Without
	// it the chart is aria-hidden (decorative).
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the chart's root element:
	// the <svg> itself, or the "no trend data" <span> when Values has
	// fewer than two points. Keys the component owns are dropped:
	// class and id (use Class / ID), data-cui-*, and the sizing and
	// naming attributes the SVG derives from config (width, height,
	// viewBox, xmlns, role, aria-labelledby, aria-hidden).
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the no-trend-data label.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// Sparkline renders a tiny inline trend chart.
func Sparkline(cfg SparklineConfig) render.HTML {
	switch cfg.Color {
	case SparklinePrimary, SparklineSuccess, SparklineWarning, SparklineDanger, SparklineInfo:
	default:
		panic("ui: Sparkline unknown Color " + strconv.Quote(string(cfg.Color)) +
			`. Pick one of: "" (primary), success, warning, danger, info`)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	min, max, finiteN := finiteRange(cfg.Values)
	if finiteN < 2 {
		// Inline trend with too few points: render a calm "no trend" dash
		// rather than crashing the host that embeds it. Sanitized extras
		// first, owned keys on top so they win.
		attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-label")
		if attrs == nil {
			attrs = html.Attrs{}
		}
		attrs["data-cui-comp"] = "ui-sparkline"
		attrs["aria-label"] = i18nui.SparklineNoData.Text(ctx)
		if cfg.Class != "" {
			attrs["class"] = cfg.Class
		}
		return render.Tag("span", attrs, render.Text("—"))
	}
	switch cfg.Shape {
	case SparklineLine, SparklineArea:
	default:
		panic("ui: Sparkline unknown Shape " + string(cfg.Shape) +
			`. Pick one of: "" (line), area`)
	}
	w := cfg.Width
	if w == 0 {
		w = 120
	}
	h := cfg.Height
	if h == 0 {
		h = 32
	}

	// Map to viewBox coords. Leave a 1px margin top + bottom so the
	// line stroke doesn't get clipped.
	pad := 1.0
	plotW := float64(w)
	plotH := float64(h) - 2*pad
	n := len(cfg.Values)
	pts := make([]string, 0, n)
	firstX, lastX := -1.0, 0.0
	for i, v := range cfg.Values {
		if !finite(v) {
			continue
		}
		x := float64(i) * plotW / float64(n-1)
		y := pad + plotH - chartFrac(v, min, max)*plotH
		pts = append(pts, ftoa(x)+","+ftoa(y))
		if firstX < 0 {
			firstX = x
		}
		lastX = x
	}

	cls := "fui-sparkline"
	if cfg.Color != "" {
		cls += " fui-sparkline--" + string(cfg.Color)
	}
	if cfg.Class != "" {
		cls += " " + escapeXML(cfg.Class)
	}

	// Caller extras land after the owned attributes. render.Attr
	// validates the key and escapes the value, matching what the
	// html primitives do for map-built roots.
	extraAttrs := serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs,
		"width", "height", "viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden"))

	svgAttrs := strings.Builder{}
	svgAttrs.WriteString(`width="`)
	if cfg.FullWidth {
		svgAttrs.WriteString(`100%" height="`)
	} else {
		svgAttrs.WriteString(strconv.Itoa(w))
		svgAttrs.WriteString(`" height="`)
	}
	svgAttrs.WriteString(strconv.Itoa(h))
	svgAttrs.WriteString(`" viewBox="0 0 `)
	svgAttrs.WriteString(strconv.Itoa(w))
	svgAttrs.WriteString(` `)
	svgAttrs.WriteString(strconv.Itoa(h))
	svgAttrs.WriteString(`" class="`)
	svgAttrs.WriteString(cls)
	svgAttrs.WriteString(`" xmlns="http://www.w3.org/2000/svg"`)
	if cfg.FullWidth {
		// Stretch the viewBox to the box: the default xMidYMid meet
		// letterboxes a wide container (the drawing keeps its 220:36
		// aspect and centers, leaving dead margins). The height is
		// 1:1 by construction, so only X stretches.
		svgAttrs.WriteString(` preserveAspectRatio="none"`)
	}
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
	var pathD strings.Builder
	pathD.WriteString("M" + pts[0])
	for i := 1; i < len(pts); i++ {
		pathD.WriteString(" L" + pts[i])
	}
	areaD := pathD.String() + " L" + ftoa(lastX) + "," + ftoa(float64(h)) +
		" L" + ftoa(firstX) + "," + ftoa(float64(h)) + " Z"

	var body string
	if cfg.Shape == SparklineArea {
		body = `<path d="` + areaD + `" class="fui-sparkline__area"/><path d="` +
			pathD.String() + `" class="fui-sparkline__line"/>`
	} else {
		body = `<path d="` + pathD.String() + `" class="fui-sparkline__line"/>`
	}

	out := `<svg ` + svgAttrs.String() + ` data-cui-comp="ui-sparkline"` + extraAttrs + `>` + body + `</svg>`
	//gofastr:allow(GOFASTR1403) every interpolated value is escaped above: escapeXML on the class, id and labelledby, render.Attr on the extras, and numbers formatted by strconv/ftoa
	return sparklineStyle.WrapHTML(render.HTML(out))
}

// chartNumber writes a value a reader sees (a tick, a bar's value, a
// gauge reading) in the reader's locale, to the two decimals ftoa keeps
// for coordinates.
func chartNumber(ctx context.Context, f float64) string {
	return i18n.Number(ctx, f, i18n.MaxFraction(2))
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
	return `[data-cui-comp="ui-sparkline"] {
  display: inline-block;
  vertical-align: middle;
  color: var(--color-primary, #18181B);
}
[data-cui-comp="ui-sparkline"] .fui-sparkline__line {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linejoin: round;
  stroke-linecap: round;
  /* FullWidth stretches the viewBox with preserveAspectRatio none; the
     stroke stays 1.5px instead of stretching with it. */
  vector-effect: non-scaling-stroke;
}
[data-cui-comp="ui-sparkline"] .fui-sparkline__area {
  fill: currentColor;
  opacity: var(--opacity-faint, 0.2);
  stroke: none;
}

/* Color presets — recolor via currentColor on the SVG root. */
.fui-sparkline.fui-sparkline--success { color: var(--color-success, #166534); }
.fui-sparkline.fui-sparkline--warning { color: var(--color-warning, #854D0E); }
.fui-sparkline.fui-sparkline--danger  { color: var(--color-danger, #B91C1C); }
.fui-sparkline.fui-sparkline--info    { color: var(--color-info, #1D4ED8); }`
}
