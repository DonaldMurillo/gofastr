package ui

import (
	"strings"
	"testing"
)

func TestPieChartEmptyRendersEmptyState(t *testing.T) {
	// No slices is a normal data-bound zero state, not misuse.
	h := string(PieChart(PieChartConfig{}))
	if !strings.Contains(h, `data-cui-comp="ui-chart-empty"`) {
		t.Errorf("empty PieChart should render the chart empty state:\n%s", h)
	}
}

func TestPieChartAllZeroRendersEmptyState(t *testing.T) {
	// Every slice zero: nothing to draw, but a legitimate empty state.
	h := string(PieChart(PieChartConfig{Slices: []PieSlice{{Value: 0}, {Value: 0}}}))
	if !strings.Contains(h, `data-cui-comp="ui-chart-empty"`) {
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

func TestPieChartExtraAttrsOnRoot(t *testing.T) {
	h := PieChart(PieChartConfig{
		Slices:     []PieSlice{{Label: "A", Value: 2}, {Label: "B", Value: 1}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("PieChart root missing data-test:\n%s", root)
	}
}

// A slice that is the whole pie starts and ends on one point, and an SVG
// arc between a point and itself draws nothing: a pie with one value, the
// most common first dataset, was an empty square. It is drawn as a closed
// circle (a donut's hole wound the other way), and so is a slice beside a
// sliver too thin to move the written coordinates.
func TestPieChartWholePieSliceDrawsACircle(t *testing.T) {
	const disc = `d="M80,2 A78,78 0 1 1 80,158 A78,78 0 1 1 80,2 Z"`
	for name, slices := range map[string][]PieSlice{
		"one slice":             {{Label: "All", Value: 10}},
		"one slice has a value": {{Value: 0}, {Label: "All", Value: 10}, {Value: 0}},
		"a sliver beside it":    {{Label: "Most", Value: 1e6}, {Label: "Rest", Value: 1}},
	} {
		h := string(PieChart(PieChartConfig{Slices: slices}))
		if !strings.Contains(h, disc) {
			t.Errorf("%s: the whole-pie slice is not a closed circle:\n%s", name, h)
		}
	}
	h := string(PieChart(PieChartConfig{InnerRadius: 0.5, Slices: []PieSlice{{Label: "All", Value: 10}}}))
	const ring = `d="M80,2 A78,78 0 1 1 80,158 A78,78 0 1 1 80,2 Z M80,41 A39,39 0 1 0 80,119 A39,39 0 1 0 80,41 Z"`
	if !strings.Contains(h, ring) {
		t.Errorf("a whole donut is not a ring:\n%s", h)
	}
	// Two halves are two ordinary slices: neither is the whole pie.
	h = string(PieChart(PieChartConfig{Slices: []PieSlice{{Value: 1}, {Value: 1}}}))
	if strings.Count(h, "<path ") != 2 || strings.Contains(h, disc) {
		t.Errorf("two equal slices must stay two half-pie paths:\n%s", h)
	}
}
