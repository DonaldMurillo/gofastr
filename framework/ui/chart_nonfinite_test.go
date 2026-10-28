package ui_test

import (
	"math"
	"regexp"
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestChartsNeverWriteNaN pins the 2026-10-27 bug bash findings U11 (a
// LineChart with one label divided by nl-1 and wrote x="NaN") and U13
// (NaN or Inf values blanked Bar, Line, Pie and Sparkline). Every chart
// treats a non-finite reading as one that never arrived, the way Gauge,
// Heatmap and Scatter do, and none writes a non-finite number into its
// SVG. A span too wide for a float64 (max - min overflows) is the same
// shape from finite inputs.
func TestChartsNeverWriteNaN(t *testing.T) {
	// Word-bounded so a class or token such as "info" never matches.
	nonFinite := regexp.MustCompile(`\bNaN\b|[+-]?\bInf\b`)
	nan, inf := math.NaN(), math.Inf(1)
	cases := map[string]func() string{
		"line one label": func() string {
			return string(ui.LineChart(ui.LineChartConfig{
				Labels: []string{"Mon"},
				Series: []ui.LineSeries{{Name: "s", Values: []float64{1, 2}}},
			}))
		},
		"line nan": func() string {
			return string(ui.LineChart(ui.LineChartConfig{
				Series: []ui.LineSeries{{Name: "s", Values: []float64{1, nan, 3}}},
			}))
		},
		"line inf area": func() string {
			return string(ui.LineChart(ui.LineChartConfig{
				Series: []ui.LineSeries{{Name: "s", Values: []float64{inf, 1, 3}, Area: true}},
			}))
		},
		"line nan first": func() string {
			return string(ui.LineChart(ui.LineChartConfig{
				Series: []ui.LineSeries{{Name: "s", Values: []float64{nan, 1, 3}}},
			}))
		},
		"line wide span": func() string {
			return string(ui.LineChart(ui.LineChartConfig{
				Series: []ui.LineSeries{{Name: "s", Values: []float64{-math.MaxFloat64, math.MaxFloat64}}},
			}))
		},
		"bar nan": func() string {
			return string(ui.BarChart(ui.BarChartConfig{
				Bars: []ui.BarChartBar{{Label: "a", Value: 1}, {Label: "b", Value: nan}},
			}))
		},
		"bar inf": func() string {
			return string(ui.BarChart(ui.BarChartConfig{
				Bars: []ui.BarChartBar{{Label: "a", Value: 1}, {Label: "b", Value: inf}},
			}))
		},
		"pie nan": func() string {
			return string(ui.PieChart(ui.PieChartConfig{
				Slices: []ui.PieSlice{{Label: "a", Value: 1}, {Label: "b", Value: nan}},
			}))
		},
		"pie inf": func() string {
			return string(ui.PieChart(ui.PieChartConfig{
				Slices: []ui.PieSlice{{Label: "a", Value: 1}, {Label: "b", Value: inf}},
			}))
		},
		"sparkline nan": func() string {
			return string(ui.Sparkline(ui.SparklineConfig{Values: []float64{1, nan, 3}}))
		},
		"sparkline inf area": func() string {
			return string(ui.Sparkline(ui.SparklineConfig{Values: []float64{1, inf, 3}, Shape: ui.SparklineArea}))
		},
		"sparkline wide span": func() string {
			return string(ui.Sparkline(ui.SparklineConfig{Values: []float64{-math.MaxFloat64, math.MaxFloat64}}))
		},
		"gauge nan": func() string {
			return string(ui.Gauge(ui.GaugeConfig{Label: "g", Value: nan}))
		},
		"heatmap nan": func() string {
			return string(ui.Heatmap(ui.HeatmapConfig{
				Rows: []string{"r"}, Cols: []string{"a", "b"},
				Values: [][]float64{{1, nan}},
			}))
		},
		"scatter inf": func() string {
			return string(ui.ScatterChart(ui.ScatterChartConfig{
				Series: []ui.ScatterSeries{{Name: "s", Points: []ui.ScatterPoint{{X: 1, Y: 1}, {X: inf, Y: 2}}}},
			}))
		},
	}
	for name, render := range cases {
		t.Run(name, func(t *testing.T) {
			out := render()
			if bad := nonFinite.FindString(out); bad != "" {
				t.Errorf("chart wrote %q into its SVG:\n%s", bad, out)
			}
		})
	}
}

// TestChartsDrawTheReadingsThatArrived: a non-finite reading is skipped,
// not a reason to blank the chart. The line still draws, the other bar
// and slice still render, and a series of one finite point is the
// not-enough-data state.
func TestChartsDrawTheReadingsThatArrived(t *testing.T) {
	nan := math.NaN()
	line := string(ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{{Name: "s", Values: []float64{1, nan, 3}}},
	}))
	if !strings.Contains(line, `class="fui-line-chart__line`) {
		t.Errorf("a line with one NaN drew no line:\n%s", line)
	}
	if strings.Count(line, " L") != 1 {
		t.Errorf("the line should join the two finite points with one segment:\n%s", line)
	}
	lone := string(ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{{Name: "s", Values: []float64{nan, 2}}},
	}))
	if strings.Contains(lone, "<path") {
		t.Errorf("a series with one finite point drew a path:\n%s", lone)
	}
	bar := string(ui.BarChart(ui.BarChartConfig{
		ShowLabels: true,
		Bars:       []ui.BarChartBar{{Label: "a", Value: 1}, {Label: "b", Value: nan}},
	}))
	if strings.Count(bar, `class="fui-bar-chart__bar`) != 1 {
		t.Errorf("want one bar for the one finite value:\n%s", bar)
	}
	if !strings.Contains(bar, ">b<") {
		t.Errorf("the missing bar's label should keep its slot:\n%s", bar)
	}
	// An Inf reading must not become the axis maximum: the finite bar
	// keeps the height it has beside a zero, not a 2px nub.
	height := regexp.MustCompile(`class="fui-bar-chart__bar[^"]*"`)
	barOf := func(v float64) string {
		h := string(ui.BarChart(ui.BarChartConfig{Bars: []ui.BarChartBar{{Label: "a", Value: 1}, {Label: "b", Value: v}}}))
		i := height.FindStringIndex(h)
		if i == nil {
			t.Fatalf("no bar rendered:\n%s", h)
		}
		return h[strings.LastIndex(h[:i[0]], "<rect"):i[0]]
	}
	if got, want := barOf(math.Inf(1)), barOf(0); got != want {
		t.Errorf("beside an Inf reading the finite bar is %s, want %s", got, want)
	}
	pie := string(ui.PieChart(ui.PieChartConfig{
		Slices: []ui.PieSlice{{Label: "a", Value: 1}, {Label: "b", Value: nan}},
	}))
	if !strings.Contains(pie, "<path") && !strings.Contains(pie, "<circle") {
		t.Errorf("the finite slice did not render:\n%s", pie)
	}
	spark := string(ui.Sparkline(ui.SparklineConfig{Values: []float64{nan, 2}}))
	if strings.Contains(spark, "<svg") {
		t.Errorf("a sparkline with one finite point drew a trend:\n%s", spark)
	}
}

// TestLineChartLabelsSitOnPoints: label i sits over point i of the
// longest series, so a label list shorter than the series lines up
// instead of stretching across the plot, and only the label on the last
// point anchors to the end.
func TestLineChartLabelsSitOnPoints(t *testing.T) {
	h := string(ui.LineChart(ui.LineChartConfig{
		Width:  300,
		Series: []ui.LineSeries{{Name: "S", Values: []float64{1, 2, 3, 4}}},
		Labels: []string{"Q1", "Q2"},
	}))
	if !strings.Contains(h, `<text x="100" `) {
		t.Errorf("Q2 should sit over the second of four points (x=100):\n%s", h)
	}
	if strings.Contains(h, `text-anchor="end">Q2`) {
		t.Errorf("Q2 is not on the last point and must not anchor end:\n%s", h)
	}
	extra := string(ui.LineChart(ui.LineChartConfig{
		Series: []ui.LineSeries{{Name: "S", Values: []float64{1, 2}}},
		Labels: []string{"a", "b", "c"},
	}))
	if strings.Contains(extra, ">c<") {
		t.Errorf("a label past the last point should be dropped:\n%s", extra)
	}
}
