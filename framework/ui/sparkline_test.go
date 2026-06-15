package ui

import (
	"strings"
	"testing"
)

func TestSparklineTooFewPointsRendersDash(t *testing.T) {
	// Too few points renders a calm inline "no trend" dash, never a panic —
	// a sparkline embedded in a row must not take the page down.
	h := string(Sparkline(SparklineConfig{Values: []float64{1}}))
	if strings.Contains(h, "<svg ") {
		t.Errorf("sparse Sparkline should not emit an svg:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-comp="ui-sparkline"`) {
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
	if !strings.Contains(h, `data-fui-comp="ui-sparkline"`) {
		t.Errorf("svg should carry data-fui-comp marker:\n%s", h)
	}
}

func TestSparklineAreaShapeAddsAreaPath(t *testing.T) {
	h := string(Sparkline(SparklineConfig{
		Values: []float64{1, 2, 3}, Shape: SparklineArea,
	}))
	if !strings.Contains(h, "ui-sparkline__area") {
		t.Errorf("area shape should add .ui-sparkline__area path:\n%s", h)
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
	if !strings.Contains(h, "ui-sparkline--danger") {
		t.Errorf("Color=danger should add modifier class:\n%s", h)
	}
}
