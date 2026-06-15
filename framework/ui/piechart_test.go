package ui

import (
	"strings"
	"testing"
)

func TestPieChartEmptyRendersEmptyState(t *testing.T) {
	// No slices is a normal data-bound zero state, not misuse.
	h := string(PieChart(PieChartConfig{}))
	if !strings.Contains(h, `data-fui-comp="ui-chart-empty"`) {
		t.Errorf("empty PieChart should render the chart empty state:\n%s", h)
	}
}

func TestPieChartAllZeroRendersEmptyState(t *testing.T) {
	// Every slice zero — nothing to draw, but a legitimate empty state.
	h := string(PieChart(PieChartConfig{Slices: []PieSlice{{Value: 0}, {Value: 0}}}))
	if !strings.Contains(h, `data-fui-comp="ui-chart-empty"`) {
		t.Errorf("all-zero PieChart should render the chart empty state:\n%s", h)
	}
}

func TestPieChartRejectsNegative(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("PieChart with negative Value should panic")
		}
	}()
	PieChart(PieChartConfig{Slices: []PieSlice{{Value: -1}, {Value: 2}}})
}

func TestPieChartEmitsOnePathPerSlice(t *testing.T) {
	h := string(PieChart(PieChartConfig{
		Slices: []PieSlice{
			{Label: "A", Value: 30},
			{Label: "B", Value: 20},
			{Label: "C", Value: 50},
		},
	}))
	if c := strings.Count(h, "<path "); c != 3 {
		t.Errorf("expected 3 slice <path>s, got %d:\n%s", c, h)
	}
}

func TestPieChartLabelEmitsTitle(t *testing.T) {
	h := string(PieChart(PieChartConfig{
		Slices: []PieSlice{{Label: "Sales", Value: 1}, {Value: 1}},
	}))
	if !strings.Contains(h, "<title>Sales</title>") {
		t.Errorf("Slice with Label should embed <title> for AT:\n%s", h)
	}
}

func TestPieChartDonutAddsCenterLabel(t *testing.T) {
	h := string(PieChart(PieChartConfig{
		Slices:        []PieSlice{{Value: 3}, {Value: 7}},
		InnerRadius:   0.6,
		CenterLabel:   "70%",
		CenterSubtext: "Adoption",
	}))
	if !strings.Contains(h, "70%") {
		t.Errorf("CenterLabel should render:\n%s", h)
	}
	if !strings.Contains(h, "Adoption") {
		t.Errorf("CenterSubtext should render:\n%s", h)
	}
}

func TestPieChartDefaultAriaHidden(t *testing.T) {
	h := string(PieChart(PieChartConfig{Slices: []PieSlice{{Value: 1}, {Value: 1}}}))
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("default PieChart should be aria-hidden:\n%s", h)
	}
}

func TestPieChartLabelledByAddsRole(t *testing.T) {
	h := string(PieChart(PieChartConfig{
		Slices:     []PieSlice{{Value: 1}, {Value: 1}},
		LabelledBy: "kpi-1",
	}))
	if !strings.Contains(h, `role="img"`) {
		t.Errorf("LabelledBy should set role=img:\n%s", h)
	}
	if !strings.Contains(h, `aria-labelledby="kpi-1"`) {
		t.Errorf("LabelledBy should set aria-labelledby:\n%s", h)
	}
}
