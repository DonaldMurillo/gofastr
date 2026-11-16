package ui

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func TestSweepChartsBarThicknessIsCapped(t *testing.T) {
	h := string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 3}}}))
	if !strings.Contains(h, `width="56"`) {
		t.Errorf("a lone bar in 320px must be drawn 56px thick, not fill its slot:\n%s", h)
	}
}

func TestSweepChartsTinyBarKeepsANub(t *testing.T) {
	h := string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 100000}, {Label: "b", Value: 1}}}))
	heights := regexp.MustCompile(`<rect [^>]*height="([0-9.]+)"`).FindAllStringSubmatch(h, -1)
	if len(heights) != 2 {
		t.Fatalf("want two bars, got %d:\n%s", len(heights), h)
	}
	if got, _ := strconv.ParseFloat(heights[1][1], 64); got < 2 {
		t.Errorf("a tiny non-zero bar is %vpx tall, want at least 2:\n%s", got, h)
	}
}

func TestSweepChartsDonutHoleIsClamped(t *testing.T) {
	slices := []PieSlice{{Label: "a", Value: 1}, {Label: "b", Value: 2}}
	big := string(PieChart(PieChartConfig{Slices: slices, InnerRadius: 3}))
	if !strings.Contains(big, " A74.1,74.1 ") {
		t.Errorf("an InnerRadius past 0.95 must cut a 0.95 hole (radius 74.1 of 78):\n%s", big)
	}
	neg := string(PieChart(PieChartConfig{Slices: slices, InnerRadius: -1}))
	if !strings.Contains(neg, `<path d="M80,80 L`) {
		t.Errorf("a negative InnerRadius must draw a plain pie:\n%s", neg)
	}
}

func TestSweepChartsSparklineKeepsStrokeInside(t *testing.T) {
	h := string(Sparkline(SparklineConfig{Values: []float64{0, 10, 0}}))
	for _, m := range regexp.MustCompile(`[ML][0-9.]+,([0-9.]+)`).FindAllStringSubmatch(h, -1) {
		if y, _ := strconv.ParseFloat(m[1], 64); y < 1 || y > 31 {
			t.Errorf("a line point at y=%v leaves the 1px margin of a 32px sparkline:\n%s", y, h)
		}
	}
}

func TestSweepChartsCounterHandsNoSecondSeed(t *testing.T) {
	h := string(Counter(CounterConfig{SignalName: "qty"}))
	if strings.Contains(h, "data-hui-counter-value") {
		t.Errorf("Counter must leave the signal's seed to the page, not hand the runtime its own:\n%s", h)
	}
}

func TestSweepChartsAgoFutureIsNowAndDatesAreUTC(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if got, want := Ago(ctx, now, now.Add(3*time.Hour)), Ago(ctx, now, now); got != want {
		t.Errorf("a time after now reads %q, want %q", got, want)
	}
	kiritimati := time.FixedZone("LINT", 14*3600)
	old := time.Date(2026, 3, 5, 23, 0, 0, 0, time.UTC).In(kiritimati)
	if got := Ago(ctx, now, old); got != "Mar 5, 2026" {
		t.Errorf("past a month Ago writes the UTC date, got %q, want Mar 5, 2026", got)
	}
}

func TestSweepChartsWriteNoStyleAttribute(t *testing.T) {
	raw := "#123456"
	out := map[string]string{
		"bar":     string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 1, Color: raw}}, ShowAxis: true, ShowLabels: true})),
		"line":    string(LineChart(LineChartConfig{Series: []LineSeries{{Name: "s", Values: []float64{1, 2}, Color: raw, Area: true}}, ShowLegend: true, Labels: []string{"a", "b"}})),
		"pie":     string(PieChart(PieChartConfig{Slices: []PieSlice{{Label: "a", Value: 1, Color: raw}, {Label: "b", Value: 1}}, InnerRadius: 0.5, CenterLabel: "x"})),
		"scatter": string(ScatterChart(ScatterChartConfig{Series: []ScatterSeries{{Name: "s", Points: []ScatterPoint{{X: 1, Y: 2}}, Color: raw}}, ShowLegend: true})),
		"heatmap": string(Heatmap(HeatmapConfig{Rows: []string{"r"}, Cols: []string{"c"}, Values: [][]float64{{1}}, ShowLegend: true})),
		"spark":   string(Sparkline(SparklineConfig{Values: []float64{1, 2}, FullWidth: true, Shape: SparklineArea})),
		"gauge":   string(Gauge(GaugeConfig{Label: "CPU", Value: 50})),
		"water":   string(Waterfall(WaterfallConfig{Label: "req", Steps: []WaterfallStep{{Label: "a", Dur: time.Millisecond}}})),
	}
	for name, h := range out {
		if strings.Contains(h, " style=") {
			t.Errorf("%s writes a style attribute; a raw color or a position is an SVG attribute:\n%s", name, h)
		}
	}
}

func TestSweepChartsSVGRootParity(t *testing.T) {
	extra := html.Attrs{"data-test": "hook", "role": "button", "aria-hidden": "false"}
	charts := map[string]func(labelledBy string) string{
		"bar": func(l string) string {
			return string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 1}}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
		"line": func(l string) string {
			return string(LineChart(LineChartConfig{Series: []LineSeries{{Name: "s", Values: []float64{1, 2}}}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
		"pie": func(l string) string {
			return string(PieChart(PieChartConfig{Slices: []PieSlice{{Label: "a", Value: 1}}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
		"scatter": func(l string) string {
			return string(ScatterChart(ScatterChartConfig{Series: []ScatterSeries{{Name: "s", Points: []ScatterPoint{{X: 1, Y: 1}}}}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
		"heatmap": func(l string) string {
			return string(Heatmap(HeatmapConfig{Rows: []string{"r"}, Cols: []string{"c"}, Values: [][]float64{{1}}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
		"spark": func(l string) string {
			return string(Sparkline(SparklineConfig{Values: []float64{1, 2}, LabelledBy: l, ID: "c", ExtraAttrs: extra}))
		},
	}
	for name, draw := range charts {
		for _, l := range []string{"", "title"} {
			h := draw(l)
			root := h[strings.Index(h, "<svg"):]
			root = root[:strings.Index(root, ">")+1]
			want := `aria-hidden="true"`
			if l != "" {
				want = `role="img" aria-labelledby="title"`
			}
			for _, must := range []string{want, `id="c"`, `data-test="hook"`, `xmlns="http://www.w3.org/2000/svg"`} {
				if !strings.Contains(root, must) {
					t.Errorf("%s (LabelledBy %q) root lacks %s:\n%s", name, l, must, root)
				}
			}
			if strings.Contains(root, `role="button"`) || strings.Contains(root, `aria-hidden="false"`) {
				t.Errorf("%s root let an owned key through ExtraAttrs:\n%s", name, root)
			}
		}
	}
}

func TestSweepChartsSeriesColorParity(t *testing.T) {
	marks := func(color string) map[string]string {
		return map[string]string{
			"bar":     string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 1, Color: color}}})),
			"scatter": string(ScatterChart(ScatterChartConfig{Series: []ScatterSeries{{Name: "s", Points: []ScatterPoint{{X: 1, Y: 1}}, Color: color}}})),
			"line":    string(LineChart(LineChartConfig{Series: []LineSeries{{Name: "s", Values: []float64{1, 2}, Color: color}}})),
			"pie":     string(PieChart(PieChartConfig{Slices: []PieSlice{{Label: "a", Value: 1, Color: color}}})),
		}
	}
	for name, h := range marks("info") {
		if !strings.Contains(h, "--info") {
			t.Errorf("%s: the palette token info is not a palette class:\n%s", name, h)
		}
	}
	for name, h := range marks("#123456") {
		if !strings.Contains(h, `="#123456"`) || strings.Contains(h, "--primary") {
			t.Errorf("%s: a hex Color is not written as the raw fill or stroke:\n%s", name, h)
		}
	}
}

func TestSweepChartsAxisGutterFitsPhoneText(t *testing.T) {
	h := string(BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "a", Value: 1500}}, ShowAxis: true}))
	m := regexp.MustCompile(`<line x1="([0-9.]+)"[^>]*class="fui-bar-chart__grid"`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no gridline:\n%s", h)
	}
	if x, _ := strconv.ParseFloat(m[1], 64); x < 5*7*16.0/12+8 {
		t.Errorf("the axis gutter is %vpx, too narrow for \"2,000\" at the phone's larger text:\n%s", x, h)
	}
}

func TestSweepChartsLabelFloorIsOneRune(t *testing.T) {
	if got := wrapChartLabel("abcdef", 0); len(got) != 1 || got[0] != "…" {
		t.Errorf("a slot too narrow for a rune gets the ellipsis alone, got %q", got)
	}
}

func TestSweepChartsLabelWrapsToTwoLines(t *testing.T) {
	if got := wrapChartLabel("aa bb cc", 2); len(got) != 2 || got[1] != "b…" {
		t.Errorf("a label wraps to two lines, the second ellipsized, got %q", got)
	}
}

func TestSweepChartsScatterSameLargeValuesHaveTwoEnds(t *testing.T) {
	ticks, _ := axisTicks([]float64{1e18, 1e18})
	if len(ticks) < 2 || ticks[0] >= ticks[len(ticks)-1] {
		t.Errorf("an axis of equal large values must still have two ends, got %v", ticks)
	}
}

func TestSweepChartsLineAreaClosesUnderDrawnPoints(t *testing.T) {
	nan := math.NaN()
	h := string(LineChart(LineChartConfig{Series: []LineSeries{{Name: "s", Values: []float64{nan, 1, 2, nan}, Area: true}}}))
	if !strings.Contains(h, ` L240,200 L120,200 Z"`) {
		t.Errorf("the area must close under the first and last drawn points (x 120 and 240):\n%s", h)
	}
}
