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

// SparklineShape picks line (default) or area.
type SparklineShape string

const (
	SparklineLine SparklineShape = ""
	SparklineArea SparklineShape = "area"
)

// SparklineColor picks the theme token the line is drawn in.
type SparklineColor string

const (
	SparklinePrimary SparklineColor = ""
	SparklineSuccess SparklineColor = "success"
	SparklineWarning SparklineColor = "warning"
	SparklineDanger  SparklineColor = "danger"
	SparklineInfo    SparklineColor = "info"
)

// SparklineConfig configures a Sparkline.
type SparklineConfig struct {
	// Values are the points in order; fewer than two finite ones draw a dash.
	Values []float64
	// Width and Height in CSS pixels. Default 120×32.
	Width  int
	Height int
	// FullWidth stretches the chart to its container's width; Height stays.
	FullWidth bool
	Shape     SparklineShape
	// Color panics outside the SparklineColor constants.
	Color SparklineColor
	// LabelledBy names the chart; without it the chart is aria-hidden.
	LabelledBy string
	ID         string
	Class      string
	// ExtraAttrs go on the root, less the keys the chart derives.
	ExtraAttrs html.Attrs
	// Ctx sets the locale of the no-data label; nil is English.
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
		attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-label")
		if attrs == nil {
			attrs = html.Attrs{}
		}
		attrs["data-cui-comp"] = "ui-sparkline"
		attrs["aria-label"] = i18nui.SparklineNoData.Text(ctx)
		if cfg.Class != "" {
			attrs["class"] = cfg.Class
		}
		if cfg.ID != "" {
			attrs["id"] = cfg.ID
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

// chartNumber writes a value a reader sees in the reader's locale, to two
// decimals.
func chartNumber(ctx context.Context, f float64) string {
	return i18n.Number(ctx, f, i18n.MaxFraction(2))
}

func ftoa(f float64) string {
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
