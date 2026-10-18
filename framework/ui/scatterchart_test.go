package ui_test

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

var scatterPoint = regexp.MustCompile(`<circle cx="([-0-9.]+)" cy="([-0-9.]+)" r="4" class="fui-scatter-chart__point[^"]*"`)

// TestScatterPlotsEachPoint: every finite point is one circle, titled
// with its series, label and coordinates.
func TestScatterPlotsEachPoint(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "Plans", Points: []ui.ScatterPoint{{X: 1, Y: 2, Label: "Basic"}, {X: 3, Y: 4}}},
		{Name: "Trials", Points: []ui.ScatterPoint{{X: 5, Y: 6}}},
	}}))
	if got := len(scatterPoint.FindAllString(h, -1)); got != 3 {
		t.Fatalf("drew %d points, want 3: %s", got, h)
	}
	for _, title := range []string{"<title>Plans: Basic (1, 2)</title>", "<title>Plans: (3, 4)</title>", "<title>Trials: (5, 6)</title>"} {
		if !strings.Contains(h, title) {
			t.Errorf("missing %s", title)
		}
	}
}

// TestScatterPlacesPointsOnAxes: the domain's corners land on the plot's
// corners. At 200×100 the plot runs x 36–192 and y 8–80.
func TestScatterPlacesPointsOnAxes(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Width: 200, Height: 100, Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: 0, Y: 0}, {X: 10, Y: 10}}},
	}}))
	m := scatterPoint.FindAllStringSubmatch(h, -1)
	if len(m) != 2 {
		t.Fatalf("want 2 points: %s", h)
	}
	if m[0][1] != "36" || m[0][2] != "80" || m[1][1] != "192" || m[1][2] != "8" {
		t.Errorf("points at (%s,%s) and (%s,%s), want (36,80) and (192,8)", m[0][1], m[0][2], m[1][1], m[1][2])
	}
}

// TestScatterTicksSpanNegatives: the axes round out to clean steps on
// both sides of zero.
func TestScatterTicksSpanNegatives(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: -3, Y: 1}, {X: 7, Y: 2}}},
	}}))
	for _, tick := range []string{">-5</text>", ">0</text>", ">7.5</text>"} {
		if !strings.Contains(h, tick) {
			t.Errorf("missing tick %s in %s", tick, h)
		}
	}
}

// TestScatterSameValuesDrawSafely: one point, or points that all share
// a value, still gets a real range instead of a zero-width axis.
func TestScatterSameValuesDrawSafely(t *testing.T) {
	for _, pts := range [][]ui.ScatterPoint{{{X: 5, Y: 5}}, {{X: 0, Y: 0}, {X: 0, Y: 0}}} {
		h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{{Name: "s", Points: pts}}}))
		if strings.Contains(h, "NaN") || strings.Contains(h, "Inf") || len(scatterPoint.FindAllString(h, -1)) != len(pts) {
			t.Errorf("points %v drew badly: %s", pts, h)
		}
	}
}

// TestScatterSkipsNonFinite: a NaN or Inf coordinate is a reading that
// never arrived. It is skipped, and with none left the chart is empty.
func TestScatterSkipsNonFinite(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: 1}, {X: math.NaN(), Y: 2}, {X: 3, Y: math.Inf(1)}}},
	}}))
	if got := strings.Count(h, "<circle"); got != 1 || strings.Contains(h, "NaN") || strings.Contains(h, "Inf") {
		t.Errorf("drew %d circles, want 1 with no NaN or Inf: %s", got, h)
	}
	for _, cfg := range []ui.ScatterChartConfig{
		{},
		{Series: []ui.ScatterSeries{{Name: "s", Points: []ui.ScatterPoint{{X: math.NaN(), Y: 1}}}}},
	} {
		if h := string(ui.ScatterChart(cfg)); !strings.Contains(h, `data-cui-comp="ui-chart-empty"`) {
			t.Errorf("want the empty state for %+v, got %s", cfg, h)
		}
	}
}

// TestScatterColorFallsBack: a palette key and a CSS color are honored;
// a stray word falls back to the palette, never an invalid black fill.
func TestScatterColorFallsBack(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "a", Color: "danger", Points: []ui.ScatterPoint{{X: 1, Y: 1}}},
		{Name: "b", Color: "#ff0000", Points: []ui.ScatterPoint{{X: 2, Y: 2}}},
		{Name: "c", Color: "tomato", Points: []ui.ScatterPoint{{X: 3, Y: 3}}},
	}}))
	if !classTokenPresent(h, "fui-scatter-chart__point--danger") || !strings.Contains(h, `fill="#ff0000"`) {
		t.Errorf("palette or CSS color not honored: %s", h)
	}
	if strings.Contains(h, `fill="tomato"`) {
		t.Errorf("a bare word reached the fill: %s", h)
	}
}

// TestScatterTinyChartKeepsPlot: a chart too small for its gutters
// still gets an 8px plot on each axis rather than a negative one.
func TestScatterTinyChartKeepsPlot(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Width: 20, Height: 20, XLabel: "x", YLabel: "y", ShowLegend: true,
		Series: []ui.ScatterSeries{{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: 1}, {X: 2, Y: 2}}}}}))
	m := regexp.MustCompile(`<line x1="([-0-9.]+)" y1="([-0-9.]+)" x2="([-0-9.]+)" y2="([-0-9.]+)" class="fui-scatter-chart__axis"`).FindAllStringSubmatch(h, -1)
	if len(m) != 2 {
		t.Fatalf("want 2 axis lines: %s", h)
	}
	n := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	if w, ht := n(m[0][3])-n(m[0][1]), n(m[1][4])-n(m[1][2]); w != 8 || ht != 8 {
		t.Errorf("plot is %v×%v, want 8×8", w, ht)
	}
}

// TestScatterNeedsSeriesName: a series with no name can't be told apart
// in a title or legend, a programmer error that panics like LineChart.
func TestScatterNeedsSeriesName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic for a nameless series")
		}
	}()
	ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{{Points: []ui.ScatterPoint{{X: 1, Y: 1}}}}})
}

// TestScatterNamedByLabelledBy: a labelled chart is an image named by
// that element; an unlabelled one is hidden from assistive tech.
func TestScatterNamedByLabelledBy(t *testing.T) {
	pts := []ui.ScatterSeries{{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: 1}}}}
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: pts, LabelledBy: "t1", XLabel: "Seats", YLabel: "MRR"}))
	if attrOf(t, h, "role") != "img" || attrOf(t, h, "aria-labelledby") != "t1" {
		t.Errorf("labelled chart not a named image: %s", h)
	}
	if !strings.Contains(h, ">Seats</text>") || !strings.Contains(h, ">MRR</text>") {
		t.Errorf("axis titles not drawn: %s", h)
	}
	h = string(ui.ScatterChart(ui.ScatterChartConfig{Series: pts}))
	if attrOf(t, h, "aria-hidden") != "true" {
		t.Errorf("unlabelled chart not hidden: %s", h)
	}
}

// TestScatterHugeValuesFinish: values far larger than their spread, or
// near the float limit, still draw in bounded time with no NaN or Inf.
func TestScatterHugeValuesFinish(t *testing.T) {
	for _, pts := range [][]ui.ScatterPoint{
		{{X: 1e17, Y: 100}, {X: 1e17 + 32, Y: 200}},
		{{X: -1e308, Y: 1e308}, {X: 1e308, Y: -1e308}},
		{{X: 1, Y: 100}, {X: math.Nextafter(1, 2), Y: 200}},
		{{X: 0, Y: 100}, {X: 1e-323, Y: 200}},
	} {
		done := make(chan string, 1)
		go func() {
			done <- string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{{Name: "s", Points: pts}}}))
		}()
		select {
		case h := <-done:
			if strings.Contains(h, "NaN") || strings.Contains(h, "Inf") || strings.Count(h, "<circle") != 2 {
				t.Errorf("points %v drew badly: %s", pts, h)
			}
			if pts[0].X < 1e308 && pts[1].X < 1e308 {
				seen := map[string]bool{}
				for _, m := range scatterXTick.FindAllStringSubmatch(h, -1) {
					if seen[m[1]] {
						t.Errorf("X ticks for %v repeat %q: %s", pts, m[1], h)
					}
					seen[m[1]] = true
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("points %v never finished", pts)
		}
	}
}

var scatterXTick = regexp.MustCompile(`class="fui-scatter-chart__tick" data-cui-internal="" text-anchor="(?:middle|end)">([^<]*)</text>`)

var scatterTick = regexp.MustCompile(`<text x="([-0-9.]+)" y="[-0-9.]+" class="fui-scatter-chart__tick" data-cui-internal="" text-anchor="end">([^<]*)<`)

// TestScatterTickLabelsDiffer: small steps keep enough decimals that no
// two ticks read the same, and titles print the exact coordinates.
func TestScatterTickLabelsDiffer(t *testing.T) {
	for _, ys := range [][]float64{{0.01, 0.03}, {0, 0.004}, {0, 1e-310}, {0, 1e300}, {1e-7, 1.0001e-7}} {
		h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
			{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: ys[0]}, {X: 2, Y: ys[1]}}},
		}}))
		seen := map[string]bool{}
		var vals []float64
		for _, m := range scatterTick.FindAllStringSubmatch(h, -1) {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				t.Fatalf("tick %q: %v", m[2], err)
			}
			if ms := scatterTick.FindStringSubmatch(h); m[1] == ms[1] { // the Y gutter
				vals = append(vals, v)
			}
			if len(m[2]) > 12 || strings.Contains(m[2], "NaN") || strings.Contains(m[2], "Inf") {
				t.Errorf("ticks for %v print %q", ys, m[2])
			}
			if seen[m[2]] {
				t.Errorf("ticks for %v repeat %q: %s", ys, m[2], h)
			}
			seen[m[2]] = true
		}
		if len(seen) < 3 {
			t.Errorf("ticks for %v = %v", ys, seen)
		}
		// The ticks are evenly spaced, and so must their labels read.
		for i := 2; i < len(vals); i++ {
			if gap, first := vals[i]-vals[i-1], vals[1]-vals[0]; math.Abs(gap-first) > 1e-9*math.Abs(first) {
				t.Errorf("ticks for %v read unevenly: %v", ys, vals)
				break
			}
		}
	}
	h := string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: 0.0025, Y: 1}}},
	}}))
	if !strings.Contains(h, "<title>s: (0.0025, 1)</title>") {
		t.Errorf("title rounded the point: %s", h)
	}
	h = string(ui.ScatterChart(ui.ScatterChartConfig{Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: math.Copysign(0, -1)}, {X: 2, Y: 6e9}}},
	}}))
	if !strings.Contains(h, ">0</text>") || strings.Contains(h, "-0") || !strings.Contains(h, ">6e+09</text>") {
		t.Errorf("want ticks 0 and 6e+09, no -0: %s", h)
	}
}

// TestScatterGutterFitsLabels: the left gutter grows with the widest Y
// tick label, so it starts inside the chart and clear of the Y title.
func TestScatterGutterFitsLabels(t *testing.T) {
	h := string(ui.ScatterChart(ui.ScatterChartConfig{YLabel: "MRR", Series: []ui.ScatterSeries{
		{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: 100}, {X: 2, Y: 12500}}},
	}}))
	for _, m := range scatterTick.FindAllStringSubmatch(h, -1) {
		x, _ := strconv.ParseFloat(m[1], 64)
		if start := x - float64(len(m[2]))*7; start < 18 {
			t.Errorf("tick %q starts at x=%v, inside the Y title", m[2], start)
		}
	}
}

var scatterLegend = regexp.MustCompile(`<text x="([-0-9.]+)" y="([-0-9.]+)" class="fui-scatter-chart__legend"[^>]*>([^<]*)<`)

// TestScatterLegendWraps: legend entries that would run past the right
// edge start a new line, and the chart grows to hold it.
func TestScatterLegendWraps(t *testing.T) {
	var series []ui.ScatterSeries
	for i, name := range []string{"Enterprise annual", "Enterprise monthly", "Team annual", "Team monthly", "Starter"} {
		series = append(series, ui.ScatterSeries{Name: name, Points: []ui.ScatterPoint{{X: float64(i), Y: float64(i)}}})
	}
	h := string(ui.ScatterChart(ui.ScatterChartConfig{ShowLegend: true, Series: series}))
	height, _ := strconv.ParseFloat(attrOf(t, h, "height"), 64)
	rows := map[string]bool{}
	for _, m := range scatterLegend.FindAllStringSubmatch(h, -1) {
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		if end := x + float64(len([]rune(m[3])))*7; end > 360 || y > height {
			t.Errorf("legend %q ends at (%v, %v), outside 360×%v", m[3], end, y, height)
		}
		rows[m[2]] = true
	}
	if len(rows) < 2 {
		t.Errorf("legend did not wrap: %s", h)
	}
	top := height
	for _, m := range scatterLegend.FindAllStringSubmatch(h, -1) {
		y, _ := strconv.ParseFloat(m[2], 64)
		top = min(top, y-12)
	}
	plotted := regexp.MustCompile(`<circle cx="[-0-9.]+" cy="([-0-9.]+)" r="4" class="[^"]*"[^>]*><title>`)
	for _, m := range plotted.FindAllStringSubmatch(h, -1) {
		if cy, _ := strconv.ParseFloat(m[1], 64); cy+4 > top {
			t.Errorf("point at cy=%v runs into the legend at %v", cy, top)
		}
	}
	ticks := regexp.MustCompile(`y="([-0-9.]+)" class="fui-scatter-chart__tick"`)
	for _, m := range ticks.FindAllStringSubmatch(h, -1) {
		if y, _ := strconv.ParseFloat(m[1], 64); y > top {
			t.Errorf("tick label at y=%v runs into the legend at %v", y, top)
		}
	}
	axis := regexp.MustCompile(`<line x1="[-0-9.]+" y1="([-0-9.]+)"[^>]*class="fui-scatter-chart__axis"`)
	plotBottom := func(name string) string {
		h := string(ui.ScatterChart(ui.ScatterChartConfig{ShowLegend: true, Series: []ui.ScatterSeries{
			{Name: name, Points: []ui.ScatterPoint{{X: 1, Y: 1}}},
		}}))
		return axis.FindStringSubmatch(h)[1]
	}
	if long, short := plotBottom(strings.Repeat("Long series name ", 4)), plotBottom("s"); long != short {
		t.Errorf("one long entry took a second legend line: plot bottom %s, want %s", long, short)
	}
}
