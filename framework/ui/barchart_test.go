package ui

import (
	"strconv"
	"strings"
	"testing"
)

func TestBarChartEmptyRendersEmptyState(t *testing.T) {
	// No bars is a normal data-bound state (a brand-new user has no rows),
	// not misuse — it must render a calm empty state, never panic.
	h := string(BarChart(BarChartConfig{}))
	if !strings.Contains(h, `data-fui-comp="ui-chart-empty"`) {
		t.Errorf("empty BarChart should render the chart empty state:\n%s", h)
	}
	if strings.Contains(h, "<rect ") {
		t.Errorf("empty BarChart should not emit bars:\n%s", h)
	}
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

// Value labels ride above every bar by default so magnitudes are legible
// without hovering — the primary dashboard-readability fix.
func TestBarChartValueLabelsOnByDefault(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "A", Value: 8}, {Label: "B", Value: 12}},
	}))
	if !strings.Contains(h, "ui-bar-chart__value") {
		t.Errorf("value labels should render by default:\n%s", h)
	}
	// The rendered magnitude text must appear (12 with no separators).
	if !strings.Contains(h, ">12<") {
		t.Errorf("bar value 12 should be printed as a label:\n%s", h)
	}
}

func TestBarChartHideValuesOptOut(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars:       []BarChartBar{{Label: "A", Value: 8}},
		HideValues: true,
	}))
	if strings.Contains(h, "ui-bar-chart__value") {
		t.Errorf("HideValues=true must suppress value labels:\n%s", h)
	}
}

// Uniform data must not render as full-height slabs: with headroom the
// tallest bar sits well below the plot ceiling. We assert the rendered
// bar height is meaningfully less than the plot height.
func TestBarChartUniformDataHasHeadroom(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars:   []BarChartBar{{Label: "A", Value: 8}, {Label: "B", Value: 8}, {Label: "C", Value: 8}, {Label: "D", Value: 8}},
		Height: 180,
	}))
	// Grab the first bar's height attribute.
	idx := strings.Index(h, `class="ui-bar-chart__bar`)
	if idx < 0 {
		t.Fatalf("no bar rendered:\n%s", h)
	}
	hi := strings.LastIndex(h[:idx], ` height="`)
	if hi < 0 {
		t.Fatalf("bar has no height:\n%s", h)
	}
	rest := h[hi+len(` height="`):]
	val := rest[:strings.Index(rest, `"`)]
	bh, err := strconv.ParseFloat(val, 64)
	if err != nil {
		t.Fatalf("unparseable bar height %q: %v", val, err)
	}
	// Plot band is ~165px (180 minus the value-label top gutter). A uniform
	// bar pinned at 100% would fill it; with the nice-rounded headroom the
	// tallest bar lands near 80%, so it must be clearly short of the ceiling.
	if bh > 140 {
		t.Errorf("uniform bar height %v looks like a full-height slab (no headroom)", bh)
	}
}

// A baseline grounds the bars even without the full axis.
func TestBarChartAlwaysDrawsBaseline(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "A", Value: 3}},
	}))
	if !strings.Contains(h, "ui-bar-chart__baseline") {
		t.Errorf("baseline line should always render:\n%s", h)
	}
}

// Long category labels must wrap onto multiple <tspan> lines rather than
// truncating mid-word, and the full text stays reachable via <title>.
func TestBarChartLongLabelsWrap(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{
			{Label: "Waiting On Customer", Value: 5},
			{Label: "Resolved", Value: 8},
			{Label: "In Progress", Value: 6},
			{Label: "Escalated", Value: 4},
		},
		ShowLabels: true,
		Width:      320,
	}))
	if !strings.Contains(h, "<tspan") {
		t.Errorf("long labels should wrap using <tspan> lines:\n%s", h)
	}
	if !strings.Contains(h, "Waiting On Customer") {
		t.Errorf("full label text should survive in <title>:\n%s", h)
	}
}
// An unrecognized Color (a bare word like "draft") is not a valid CSS
// color — an SVG fill="draft" renders black. It must fall back to the
// theme primary class instead of emitting the bad value verbatim.
func TestBarChartInvalidColorFallsBack(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "x", Value: 1, Color: "draft"}},
	}))
	if !strings.Contains(h, "ui-bar-chart__bar--primary") {
		t.Errorf("unrecognized Color should fall back to primary class:\n%s", h)
	}
	if strings.Contains(h, `fill="draft"`) {
		t.Errorf("unrecognized Color must not be emitted as a raw fill:\n%s", h)
	}
}

// A hex Color is a valid CSS color and passes straight through to fill.
func TestBarChartHexColorPassesThrough(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "x", Value: 1, Color: "#ff6b35"}},
	}))
	if !strings.Contains(h, `fill="#ff6b35"`) {
		t.Errorf("hex Color should pass through as fill:\n%s", h)
	}
}

// A var(--…) Color is a valid CSS color and passes straight through.
func TestBarChartVarColorPassesThrough(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "x", Value: 1, Color: "var(--color-success)"}},
	}))
	if !strings.Contains(h, `fill="var(--color-success)"`) {
		t.Errorf("var() Color should pass through as fill:\n%s", h)
	}
}

// A registered status variant name resolves to its accent color (token
// shorthand like {colors.primary} → var(--color-primary)) and becomes
// the bar fill.
func TestBarChartStatusVariantColor(t *testing.T) {
	h := string(BarChart(BarChartConfig{
		Bars: []BarChartBar{{Label: "x", Value: 1, Color: string(testBetaStatus)}},
	}))
	if !strings.Contains(h, `fill="var(--color-primary)"`) {
		t.Errorf("registered status variant should resolve to its accent fill:\n%s", h)
	}
}
