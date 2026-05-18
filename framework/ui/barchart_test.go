package ui

import (
	"strings"
	"testing"
)

func TestBarChartRequiresBars(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("BarChart without Bars should panic")
		}
	}()
	BarChart(BarChartConfig{})
}

func TestBarChartRejectsNegative(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("BarChart with negative Value should panic")
		}
	}()
	BarChart(BarChartConfig{Bars: []BarChartBar{{Label: "x", Value: -1}}})
}

func TestBarChartEmitsRectPerBar(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{
			{Label: "A", Value: 10},
			{Label: "B", Value: 20},
			{Label: "C", Value: 30},
		},
	}))
	if c := strings.Count(h, "<rect "); c != 3 {
		t.Errorf("expected 3 <rect> bars, got %d:\n%s", c, h)
	}
}

func TestBarChartLabelEmitsTitle(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "Q1", Value: 100}},
	}))
	if !strings.Contains(h, "<title>") {
		t.Errorf("Bar with Label should embed <title>:\n%s", h)
	}
	if !strings.Contains(h, "Q1: 100") {
		t.Errorf("title should include 'Label: Value':\n%s", h)
	}
}

func TestBarChartShowLabelsEmitsText(t *testing.T) {
	on := string(BarChart(BarChartConfig{
		Bars:       []BarChartBar{{Label: "Q1", Value: 1}},
		ShowLabels: true,
	}))
	if !strings.Contains(on, "ui-bar-chart__label") {
		t.Errorf("ShowLabels=true should emit .ui-bar-chart__label text:\n%s", on)
	}
}

func TestBarChartShowAxisEmitsValueLabels(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars:     []BarChartBar{{Label: "x", Value: 100}},
		ShowAxis: true,
	}))
	if !strings.Contains(h, "ui-bar-chart__axis-label") {
		t.Errorf("ShowAxis=true should emit axis labels:\n%s", h)
	}
}

func TestBarChartColorOverridesViaPalette(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "x", Value: 1, Color: "danger"}},
	}))
	if !strings.Contains(h, "ui-bar-chart__bar--danger") {
		t.Errorf("palette Color should add modifier class:\n%s", h)
	}
}
