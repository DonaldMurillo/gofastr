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

// ─── Heatmap ────────────────────────────────────────────────────────
//
// A grid of values shaded by size: sign-ups by weekday and hour, errors
// by service and day. Each cell takes one of four strengths of one theme
// color, from the lowest value to the highest, so the busy cells stand
// out at a glance. Pure SVG, no script. Each cell's <title> names its
// row, column and value.

// HeatmapConfig configures a Heatmap.
type HeatmapConfig struct {
	// Rows and Cols label the grid's sides. A row label longer than 17
	// characters is cut with an ellipsis; the cell titles keep it whole.
	Rows, Cols []string
	// Values holds one row per Rows entry, each with one value per Cols
	// entry; a mismatch panics. NaN or Inf is a reading that never
	// arrived: the cell draws empty and its title says "no data". With
	// no reading at all the chart renders the shared empty state.
	Values [][]float64
	// Color is the palette token the cells shade: "primary" (default),
	// "info", "success", "warning" or "danger". Anything else falls back
	// to primary.
	Color string
	// Format writes a value in its cell's title. Default: the number,
	// to two decimals at most.
	Format func(float64) string
	// Width in CSS pixels; the height follows from the row count. A grid
	// whose cells would fall under 4px wide widens the chart instead.
	// Default 360.
	Width int
	// ShowLegend renders a "Less … More" strip of the four strengths
	// under the grid.
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

const (
	heatTop    = 16.0 // column labels
	heatGap    = 2.0
	heatCellH  = 20.0
	heatLegend = 24.0
	heatCharW  = 8.0 // label width per character at --text-xs, wide glyphs
	heatRowMax = 17  // row label characters before the ellipsis
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
		return chartEmpty(cfg.ID, cfg.LabelledBy, cfg.Class, "No data yet", cfg.ExtraAttrs)
	}
	format := cfg.Format
	if format == nil {
		format = ftoa
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
	// At the smallest cell the grid can outgrow Width: widen the chart
	// rather than clip the right-hand columns.
	w = max(w, int(math.Ceil(left+float64(nc)*cellW+heatGap*float64(nc-1))))
	gridH := float64(nr)*heatCellH + float64(nr-1)*heatGap
	h := heatTop + gridH
	if cfg.ShowLegend {
		h += heatLegend
	}
	// Draw every stride-th column label, so long labels over narrow
	// cells don't overlap.
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
			level, value := "empty", "no data"
			if finite(v) {
				level, value = strconv.Itoa(heatLevel(v, lo, hi)), format(v)
			}
			rect(colX(j), y, cellW, heatCellH, level)
			sb.WriteString(`><title>`)
			sb.WriteString(escapeXML(r + ", " + cfg.Cols[j] + ": " + value))
			sb.WriteString(`</title></rect>`)
		}
	}
	if cfg.ShowLegend {
		y := heatTop + gridH + 10
		x := float64(w) - 4*12 - 32
		label(x-6, y+9, "end", "Less")
		for k := 1; k <= 4; k++ {
			rect(x+float64(k-1)*12, y, 10, 10, strconv.Itoa(k))
			sb.WriteString(`/>`)
		}
		label(x+4*12+2, y+9, "start", "More")
	}
	sb.WriteString(`</svg>`)
	return heatmapStyle.WrapHTML(render.HTML(sb.String()))
}

// heatLevel places v on four steps between lo and hi: the lowest value
// is 1, the highest 4. With no spread every value is 4.
func heatLevel(v, lo, hi float64) int {
	if hi == lo {
		return 4
	}
	// Halves keep a range past the float limit finite.
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
