package ui

import (
	"context"
	"math"
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

// HeatmapConfig configures a Heatmap.
type HeatmapConfig struct {
	// Rows and Cols label the grid's sides; a row label past 17
	// characters is cut, the cell titles keep it whole.
	Rows, Cols []string
	// Values holds one row per Rows entry of one value per Cols entry; a
	// mismatch panics. A NaN or Inf cell draws empty.
	Values [][]float64
	// Color is the palette token the cells shade; anything else is primary.
	Color string
	// Format writes a value in its cell's title. Default two decimals.
	Format func(float64) string
	// Width in CSS pixels, default 360; the height follows the rows.
	Width int
	// ShowLegend renders a "Less … More" strip under the grid.
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

const (
	heatTop    = 16.0
	heatGap    = 2.0
	heatCellH  = 20.0
	heatLegend = 24.0
	heatCharW  = 8.0
	heatRowMax = 17
)

// Heatmap renders a grid of values shaded by size.
func Heatmap(cfg HeatmapConfig) render.HTML {
	if len(cfg.Values) != len(cfg.Rows) {
		panic("ui: Heatmap needs one Values row per Rows entry")
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, row := range cfg.Values {
		if len(row) != len(cfg.Cols) {
			panic("ui: Heatmap needs one value per Cols entry in every row")
		}
		for _, v := range row {
			if finite(v) {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if len(cfg.Rows) == 0 || len(cfg.Cols) == 0 || lo > hi {
		return chartEmpty(cfg.Ctx, cfg.ID, cfg.LabelledBy, cfg.Class, "", cfg.ExtraAttrs)
	}
	format := cfg.Format
	if format == nil {
		format = func(v float64) string { return chartNumber(cfg.Ctx, v) }
	}
	tone := "primary"
	if slices.Contains(chartPalette, cfg.Color) {
		tone = cfg.Color
	}

	rowText := make([]string, len(cfg.Rows))
	left := 0.0
	for i, r := range cfg.Rows {
		rowText[i] = r
		if rs := []rune(r); len(rs) > heatRowMax {
			rowText[i] = string(rs[:heatRowMax-1]) + "…"
		}
		left = max(left, float64(utf8.RuneCountInString(rowText[i]))*heatCharW)
	}
	left += 8
	w := cfg.Width
	if w <= 0 {
		w = 360
	}
	nc, nr := len(cfg.Cols), len(cfg.Rows)
	cellW := max((float64(w)-left-heatGap*float64(nc-1))/float64(nc), 4)
	w = max(w, int(math.Ceil(left+float64(nc)*cellW+heatGap*float64(nc-1))))
	gridH := float64(nr)*heatCellH + float64(nr-1)*heatGap
	h := heatTop + gridH
	if cfg.ShowLegend {
		h += heatLegend
	}
	widest := 0.0
	for _, c := range cfg.Cols {
		widest = max(widest, float64(utf8.RuneCountInString(c))*heatCharW)
	}
	stride := max(int(math.Ceil(widest/(cellW+heatGap))), 1)

	cls := "fui-heatmap fui-heatmap--" + tone
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	height := int(math.Ceil(h))
	var sb strings.Builder
	sb.WriteString(`<svg width="`)
	sb.WriteString(strconv.Itoa(w))
	sb.WriteString(`" height="`)
	sb.WriteString(strconv.Itoa(height))
	sb.WriteString(`" viewBox="0 0 `)
	sb.WriteString(strconv.Itoa(w))
	sb.WriteString(` `)
	sb.WriteString(strconv.Itoa(height))
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
	sb.WriteString(` data-cui-comp="ui-heatmap"`)
	sb.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs,
		"width", "height", "viewBox", "xmlns", "role", "aria-labelledby", "aria-hidden")))
	sb.WriteString(`>`)

	label := func(x, y float64, anchor, body string) {
		sb.WriteString(`<text x="`)
		sb.WriteString(ftoa(x))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(y))
		sb.WriteString(`" class="fui-heatmap__label" data-cui-internal="" text-anchor="`)
		sb.WriteString(anchor)
		sb.WriteString(`">`)
		sb.WriteString(escapeXML(body))
		sb.WriteString(`</text>`)
	}
	rect := func(x, y, cw, ch float64, level string) {
		sb.WriteString(`<rect x="`)
		sb.WriteString(ftoa(x))
		sb.WriteString(`" y="`)
		sb.WriteString(ftoa(y))
		sb.WriteString(`" width="`)
		sb.WriteString(ftoa(cw))
		sb.WriteString(`" height="`)
		sb.WriteString(ftoa(ch))
		sb.WriteString(`" rx="3" class="fui-heatmap__cell fui-heatmap__cell--`)
		sb.WriteString(level)
		sb.WriteString(`" data-cui-internal=""`)
	}
	colX := func(j int) float64 { return left + float64(j)*(cellW+heatGap) }
	for j, c := range cfg.Cols {
		if j%stride == 0 {
			label(colX(j)+cellW/2, heatTop-5, "middle", c)
		}
	}
	for i, r := range cfg.Rows {
		y := heatTop + float64(i)*(heatCellH+heatGap)
		label(left-6, y+heatCellH/2+4, "end", rowText[i])
		for j, v := range cfg.Values[i] {
			level, value := "empty", i18nui.HeatmapNoData.Text(cfg.Ctx)
			if finite(v) {
				level, value = strconv.Itoa(heatLevel(v, lo, hi)), format(v)
			}
			rect(colX(j), y, cellW, heatCellH, level)
			sb.WriteString(`><title>`)
			sb.WriteString(escapeXML(i18nui.HeatmapCell.Text(cfg.Ctx, map[string]any{"row": r, "col": cfg.Cols[j], "value": value})))
			sb.WriteString(`</title></rect>`)
		}
	}
	if cfg.ShowLegend {
		y := heatTop + gridH + 10
		x := float64(w) - 4*12 - 32
		label(x-6, y+9, "end", i18nui.HeatmapLess.Text(cfg.Ctx))
		for k := 1; k <= 4; k++ {
			rect(x+float64(k-1)*12, y, 10, 10, strconv.Itoa(k))
			sb.WriteString(`/>`)
		}
		label(x+4*12+2, y+9, "start", i18nui.HeatmapMore.Text(cfg.Ctx))
	}
	sb.WriteString(`</svg>`)
	return heatmapStyle.WrapHTML(render.HTML(sb.String()))
}

// heatLevel places v on steps 1 to 4 between lo and hi; no spread is 4.
func heatLevel(v, lo, hi float64) int {
	if hi == lo {
		return 4
	}
	return 1 + min(int((v/2-lo/2)/(hi/2-lo/2)*4), 3)
}

var heatmapStyle = registry.RegisterStyle("ui-heatmap", heatmapCSS)

func heatmapCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-heatmap"] {
  display: block;
  max-inline-size: 100%;
}
[data-cui-comp="ui-heatmap"] .fui-heatmap__label {
  font-size: var(--text-xs, 0.75rem);
  fill: var(--color-text-muted, #52525B);
}
.fui-heatmap--primary .fui-heatmap__cell { fill: var(--color-primary, #18181B); }
.fui-heatmap--info    .fui-heatmap__cell { fill: var(--color-info, #1D4ED8); }
.fui-heatmap--success .fui-heatmap__cell { fill: var(--color-success, #166534); }
.fui-heatmap--warning .fui-heatmap__cell { fill: var(--color-warning, #854D0E); }
.fui-heatmap--danger  .fui-heatmap__cell { fill: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-heatmap"] .fui-heatmap__cell--1 { fill-opacity: 0.25; }
[data-cui-comp="ui-heatmap"] .fui-heatmap__cell--2 { fill-opacity: 0.5; }
[data-cui-comp="ui-heatmap"] .fui-heatmap__cell--3 { fill-opacity: 0.75; }
[data-cui-comp="ui-heatmap"] .fui-heatmap__cell--4 { fill-opacity: 1; }
[data-cui-comp="ui-heatmap"] .fui-heatmap__cell.fui-heatmap__cell--empty { fill: var(--color-border, #E4E4E7); }`
}
