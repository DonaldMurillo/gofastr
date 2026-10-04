package ui

import (
	"strings"
	"testing"
)

func TestSparklineTooFewPointsRendersDash(t *testing.T) {
	// Too few points renders a calm inline "no trend" dash, never a panic,
	// a sparkline embedded in a row must not take the page down.
	h := string(Sparkline(SparklineConfig{Values: []float64{1}}))
	if strings.Contains(h, "<svg ") {
		t.Errorf("sparse Sparkline should not emit an svg:\n%s", h)
	}
	if !strings.Contains(h, `data-cui-comp="ui-sparkline"`) {
		t.Errorf("sparse Sparkline should still carry its comp marker:\n%s", h)
	}
}

func TestSparklineEmitsSVGPath(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 4, 2, 5, 3, 6, 4, 7},
	}))
	if !strings.Contains(h, "<svg ") {
		t.Errorf("expected <svg> root:\n%s", h)
	}
	if !strings.Contains(h, "<path ") {
		t.Errorf("expected at least one <path>:\n%s", h)
	}
	if !strings.Contains(h, `data-cui-comp="ui-sparkline"`) {
		t.Errorf("svg should carry data-cui-comp marker:\n%s", h)
	}
}

func TestSparklineAreaShapeAddsAreaPath(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 2, 3}, Shape: SparklineArea,
	}))
	if !classTokenPresent(h, "fui-sparkline__area") {
		t.Errorf("area shape should add .fui-sparkline__area path:\n%s", h)
	}
}

func TestSparklineDefaultIsAriaHidden(t *testing.T) {
	h := string(Sparkline(SparklineConfig{Values: []float64{1, 2}}))
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("default Sparkline should be aria-hidden (decorative):\n%s", h)
	}
}

func TestSparklineLabelledByEmitsAriaLabelledby(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 2}, LabelledBy: "kpi-1",
	}))
	if !strings.Contains(h, `role="img"`) {
		t.Errorf("LabelledBy should set role=img:\n%s", h)
	}
	if !strings.Contains(h, `aria-labelledby="kpi-1"`) {
		t.Errorf("LabelledBy should set aria-labelledby:\n%s", h)
	}
}

func TestSparklineColorPreset(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 2}, Color: "danger",
	}))
	if !classTokenPresent(h, "fui-sparkline--danger") {
		t.Errorf("Color=danger should add modifier class:\n%s", h)
	}
}

func TestSparklineExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, out := range map[string]string{
		"svg":    string(Sparkline(SparklineConfig{Values: []float64{1, 2, 3}, ExtraAttrs: extra})),
		"nodata": string(Sparkline(SparklineConfig{Values: []float64{1}, ExtraAttrs: extra})),
	} {
		root := out[:strings.Index(out, ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

// FullWidth stretches the chart to its container: the width attribute
// becomes 100% while the viewBox keeps the configured aspect (the
// fluid-card spelling — a fixed px width leaves dead margins in a
// responsive grid column).
func TestSparklineFullWidthEmitsPercentWidth(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 3, 2, 5}, Width: 220, Height: 36, FullWidth: true,
	}))
	if !strings.Contains(h, `width="100%"`) || !strings.Contains(h, `height="36"`) {
		t.Errorf("FullWidth sparkline must emit width=100%% with fixed height:\n%s", h)
	}
	if !strings.Contains(h, `preserveAspectRatio="none"`) {
		t.Errorf("FullWidth sparkline must stretch, not letterbox (preserveAspectRatio=none):\n%s", h)
	}
	if !strings.Contains(h, `viewBox="0 0 220 36"`) {
		t.Errorf("FullWidth sparkline must keep its viewBox aspect basis:\n%s", h)
	}
	fixed := string(Sparkline(SparklineConfig{Values: []float64{1, 3, 2, 5}, Width: 220, Height: 36}))
	if !strings.Contains(fixed, `width="220"`) {
		t.Errorf("default sparkline keeps its px width:\n%s", fixed)
	}
}
