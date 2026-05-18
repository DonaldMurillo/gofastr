package ui

import (
	"strings"
	"testing"
)

func TestLineChartRequiresSeries(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("LineChart without Series should panic")
		}
	}()
	LineChart(LineChartConfig{})
}

func TestLineChartSeriesRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("LineSeries without Name should panic")
		}
	}()
	LineChart(LineChartConfig{Series: []LineSeries{{Values: []float64{1, 2}}}})
}

func TestLineChartSeriesRequiresTwoPoints(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("LineSeries with <2 Values should panic")
		}
	}()
	LineChart(LineChartConfig{Series: []LineSeries{
		{Name: "S", Values: []float64{1}},
	}})
}

func TestLineChartEmitsOnePathPerSeries(t *testing.T) {
	h := string(LineChart(LineChartConfig{
		Series: []LineSeries{
			{Name: "A", Values: []float64{1, 2, 3}},
			{Name: "B", Values: []float64{3, 2, 1}},
		},
	}))
	// 2 line paths, no area paths (Area false).
	if c := strings.Count(h, "ui-line-chart__line"); c < 2 {
		t.Errorf("expected 2 line classes, got %d:\n%s", c, h)
	}
}

func TestLineChartAreaEmitsAreaPath(t *testing.T) {
	h := string(LineChart(LineChartConfig{
		Series: []LineSeries{{Name: "S", Values: []float64{1, 2, 3}, Area: true}},
	}))
	if !strings.Contains(h, "ui-line-chart__area") {
		t.Errorf("Series.Area=true should emit area path:\n%s", h)
	}
}

func TestLineChartShowLegendEmitsCircles(t *testing.T) {
	h := string(LineChart(LineChartConfig{
		ShowLegend: true,
		Series:     []LineSeries{{Name: "A", Values: []float64{1, 2}}},
	}))
	if !strings.Contains(h, "ui-line-chart__legend") {
		t.Errorf("ShowLegend should emit legend text:\n%s", h)
	}
	if !strings.Contains(h, "<circle ") {
		t.Errorf("ShowLegend should emit a swatch <circle>:\n%s", h)
	}
}

func TestLineChartXLabelsEmitText(t *testing.T) {
	h := string(LineChart(LineChartConfig{
		Series: []LineSeries{{Name: "S", Values: []float64{1, 2, 3, 4}}},
		Labels: []string{"Q1", "Q2", "Q3", "Q4"},
	}))
	if !strings.Contains(h, "Q1") || !strings.Contains(h, "Q4") {
		t.Errorf("Labels should render as <text>:\n%s", h)
	}
}
