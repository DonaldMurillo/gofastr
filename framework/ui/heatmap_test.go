package ui_test

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

var heatCell = regexp.MustCompile(`<rect [^>]*class="fui-heatmap__cell fui-heatmap__cell--([a-z0-9]+)"[^>]*><title>([^<]*)</title>`)

func heatCells(h string) [][]string { return heatCell.FindAllStringSubmatch(h, -1) }

// TestHeatmapCellPerValue: every value is one cell, titled with its row,
// column and value.
func TestHeatmapCellPerValue(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows:   []string{"Mon", "Tue"},
		Cols:   []string{"9am", "noon", "5pm"},
		Values: [][]float64{{3, 1, 0}, {2, 5, 4}},
	}))
	cells := heatCells(h)
	if len(cells) != 6 {
		t.Fatalf("drew %d cells, want 6: %s", len(cells), h)
	}
	if cells[0][2] != "Mon, 9am: 3" || cells[4][2] != "Tue, noon: 5" {
		t.Errorf("titles = %q, %q", cells[0][2], cells[4][2])
	}
	for _, label := range []string{">Mon</text>", ">Tue</text>", ">9am</text>", ">5pm</text>"} {
		if !strings.Contains(h, label) {
			t.Errorf("missing label %s", label)
		}
	}
}

// TestHeatmapLevelsByValue: the lowest value takes the faintest level and
// the highest the strongest, on four steps across the range.
func TestHeatmapLevelsByValue(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"r"}, Cols: []string{"a", "b", "c", "d", "e"},
		Values: [][]float64{{0, 1, 2, 3, 4}},
	}))
	var got []string
	for _, c := range heatCells(h) {
		got = append(got, c[1])
	}
	if strings.Join(got, ",") != "1,2,3,4,4" {
		t.Errorf("levels = %v, want 1,2,3,4,4", got)
	}
}

// TestHeatmapHugeSpanKeepsLevels: a range past the float limit still
// places the midpoint mid-scale and the top at the strongest level.
func TestHeatmapHugeSpanKeepsLevels(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"r"}, Cols: []string{"a", "b", "c"}, Values: [][]float64{{-1e308, 0, 1e308}},
	}))
	var got []string
	for _, c := range heatCells(h) {
		got = append(got, c[1])
	}
	if strings.Join(got, ",") != "1,3,4" {
		t.Errorf("levels = %v, want 1,3,4", got)
	}
}

// TestHeatmapSameValuesAreFull: with no spread every cell is the strong
// level, and the scale math never divides by zero.
func TestHeatmapSameValuesAreFull(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"r"}, Cols: []string{"a", "b"}, Values: [][]float64{{7, 7}},
	}))
	if n := len(heatCells(h)); n != 2 {
		t.Fatalf("drew %d cells, want 2: %s", n, h)
	}
	for _, c := range heatCells(h) {
		if c[1] != "4" {
			t.Errorf("level %s, want 4: %s", c[1], h)
		}
	}
	if strings.Contains(h, "NaN") {
		t.Errorf("NaN in output: %s", h)
	}
}

// TestHeatmapNaNCellIsEmpty: a missing reading is an empty cell that says
// so, and a grid with no reading at all is the shared empty state.
func TestHeatmapNaNCellIsEmpty(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"r"}, Cols: []string{"a", "b"}, Values: [][]float64{{1, math.NaN()}},
	}))
	cells := heatCells(h)
	if len(cells) != 2 || cells[1][1] != "empty" || cells[1][2] != "r, b: no data" {
		t.Errorf("NaN cell = %v", cells)
	}
	for _, cfg := range []ui.HeatmapConfig{
		{},
		{Rows: []string{"r"}, Cols: []string{"a"}, Values: [][]float64{{math.NaN()}}},
	} {
		if h := string(ui.Heatmap(cfg)); !strings.Contains(h, `data-cui-comp="ui-chart-empty"`) {
			t.Errorf("want the empty state for %+v, got %s", cfg, h)
		}
	}
}

// TestHeatmapFormatsTitles: Format writes the value in each title.
func TestHeatmapFormatsTitles(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"r"}, Cols: []string{"a"}, Values: [][]float64{{1250}},
		Format: func(v float64) string { return "$" + strconv.FormatFloat(v, 'f', 2, 64) },
	}))
	if c := heatCells(h); len(c) != 1 || c[0][2] != "r, a: $1250.00" {
		t.Errorf("cells = %v", c)
	}
}

// TestHeatmapToneAndName: the tone picks the palette class (a stray word
// falls back to primary), and LabelledBy names the chart.
func TestHeatmapToneAndName(t *testing.T) {
	grid := ui.HeatmapConfig{Rows: []string{"r"}, Cols: []string{"a"}, Values: [][]float64{{1}}, LabelledBy: "t1"}
	grid.Color = "success"
	h := string(ui.Heatmap(grid))
	if !classTokenPresent(h, "fui-heatmap--success") || attrOf(t, h, "aria-labelledby") != "t1" || attrOf(t, h, "role") != "img" {
		t.Errorf("tone or name missing: %s", h)
	}
	grid.Color = "tomato"
	if h := string(ui.Heatmap(grid)); !classTokenPresent(h, "fui-heatmap--primary") {
		t.Errorf("stray color did not fall back: %s", h)
	}
}

// TestHeatmapRefusesRaggedValues: Values must match Rows × Cols, or the
// labels name the wrong cells. A mismatch panics.
func TestHeatmapRefusesRaggedValues(t *testing.T) {
	for name, cfg := range map[string]ui.HeatmapConfig{
		"rows": {Rows: []string{"a"}, Cols: []string{"x"}, Values: [][]float64{{1}, {2}}},
		"cols": {Rows: []string{"a"}, Cols: []string{"x", "y"}, Values: [][]float64{{1}}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			ui.Heatmap(cfg)
		})
	}
}

// TestHeatmapRowLabelsFit: a row label ends far enough right that wide
// glyphs (W, M) at --text-xs fit inside the chart, about 8px each.
func TestHeatmapRowLabelsFit(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"Wed"}, Cols: []string{"a"}, Values: [][]float64{{1}},
	}))
	m := regexp.MustCompile(`<text x="([0-9.]+)"[^>]*text-anchor="end">Wed<`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no Wed label: %s", h)
	}
	if x, _ := strconv.ParseFloat(m[1], 64); x < 3*8 {
		t.Errorf("Wed label ends at x=%v, want at least 24", x)
	}
}

// TestHeatmapWidensForColumns: a grid wider than Width at the smallest
// cell widens the chart rather than clipping its right-hand columns.
func TestHeatmapWidensForColumns(t *testing.T) {
	cols := make([]string, 40)
	vals := make([]float64, 40)
	for i := range cols {
		cols[i], vals[i] = strconv.Itoa(i), float64(i)
	}
	h := string(ui.Heatmap(ui.HeatmapConfig{Rows: []string{"r"}, Cols: cols, Values: [][]float64{vals}, Width: 200}))
	width, _ := strconv.ParseFloat(attrOf(t, h, "width"), 64)
	m := regexp.MustCompile(`<rect x="([0-9.]+)" y="[0-9.]+" width="([0-9.]+)"`).FindAllStringSubmatch(h, -1)
	last := m[len(m)-1]
	x, _ := strconv.ParseFloat(last[1], 64)
	cw, _ := strconv.ParseFloat(last[2], 64)
	if x+cw > width || cw != 4 {
		t.Errorf("last cell is %vpx wide ending at %v; want 4px within the chart's width %v", cw, x+cw, width)
	}
}

// TestHeatmapThinsColumnLabels: column labels too wide for their cells
// are drawn every few columns, starting with the first.
func TestHeatmapThinsColumnLabels(t *testing.T) {
	cols := make([]string, 24)
	vals := make([]float64, 24)
	for i := range cols {
		cols[i] = "Hour " + strconv.Itoa(10+i)
	}
	h := string(ui.Heatmap(ui.HeatmapConfig{Rows: []string{"r"}, Cols: cols, Values: [][]float64{vals}}))
	n := strings.Count(h, `text-anchor="middle">Hour `)
	if !strings.Contains(h, `text-anchor="middle">Hour 10<`) || n == 0 || n >= 24 {
		t.Errorf("drew %d of 24 column labels, want the first and fewer than all: %s", n, h)
	}
}

// TestHeatmapTruncatesLongRows: a row label too long for the gutter is
// cut with an ellipsis; the cell titles keep the full name.
func TestHeatmapTruncatesLongRows(t *testing.T) {
	h := string(ui.Heatmap(ui.HeatmapConfig{
		Rows: []string{"payments-webhook-worker"}, Cols: []string{"a"}, Values: [][]float64{{1}},
	}))
	m := regexp.MustCompile(`<text x="([0-9.]+)"[^>]*text-anchor="end">([^<]*)<`).FindStringSubmatch(h)
	if m == nil || !strings.HasSuffix(m[2], "…") {
		t.Fatalf("label not truncated: %s", h)
	}
	if x, _ := strconv.ParseFloat(m[1], 64); x-float64(len([]rune(m[2])))*8 < 0 {
		t.Errorf("label %q at x=%v starts off the chart", m[2], x)
	}
	if c := heatCells(h); len(c) != 1 || c[0][2] != "payments-webhook-worker, a: 1" {
		t.Errorf("title lost the full name: %v", c)
	}
}
