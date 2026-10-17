package ui_test

import (
	"math"
	"regexp"
	"strings"
	"testing"

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
