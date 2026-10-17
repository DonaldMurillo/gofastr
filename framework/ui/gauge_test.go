package ui_test

import (
	"math"
	"regexp"
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

func attrOf(t *testing.T, h, name string) string {
	t.Helper()
	m := regexp.MustCompile(` ` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no %s attribute in %s", name, h)
	}
	return m[1]
}

// TestGaugeIsANamedMeter: the gauge is a meter with a name and a range
// assistive tech reads, not an unlabeled picture.
func TestGaugeIsANamedMeter(t *testing.T) {
	h := string(ui.Gauge(ui.GaugeConfig{Label: "CPU", Value: 42, ValueText: "42%"}))
	for name, want := range map[string]string{
		"role": "meter", "aria-label": "CPU", "aria-valuemin": "0",
		"aria-valuemax": "100", "aria-valuenow": "42", "aria-valuetext": "42%",
	} {
		if got := attrOf(t, h, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(h, ">42%</text>") || !strings.Contains(h, ">CPU</span>") {
		t.Errorf("value text or label not drawn: %s", h)
	}
}

// TestGaugeClampsValue: a reading past the range pins the arc and
// aria-valuenow to the end, while the text still says what was read.
func TestGaugeClampsValue(t *testing.T) {
	h := string(ui.Gauge(ui.GaugeConfig{Label: "Load", Value: 150, ValueText: "150%"}))
	if got := attrOf(t, h, "aria-valuenow"); got != "100" {
		t.Errorf("aria-valuenow = %q, want 100", got)
	}
	if got := attrOf(t, h, "aria-valuetext"); got != "150%" {
		t.Errorf("aria-valuetext = %q, want 150%%", got)
	}
	h = string(ui.Gauge(ui.GaugeConfig{Label: "Load", Value: -5, Min: 0, Max: 10}))
	if got := attrOf(t, h, "aria-valuenow"); got != "0" {
		t.Errorf("below-range aria-valuenow = %q, want 0", got)
	}
	if strings.Contains(h, "fui-gauge__value") {
		t.Errorf("an empty reading drew a value arc: %s", h)
	}
}

// TestGaugeArcEndsAtValue: half the range ends the arc at the top of the
// semicircle, the whole range at its right end.
func TestGaugeArcEndsAtValue(t *testing.T) {
	end := func(v float64) string {
		h := string(ui.Gauge(ui.GaugeConfig{Label: "x", Value: v, Size: 200}))
		m := regexp.MustCompile(`class="fui-gauge__value[^"]*" d="M[^A]+A[0-9.]+,[0-9.]+ 0 0 1 ([0-9.]+,[0-9.]+)"`).FindStringSubmatch(h)
		if m == nil {
			t.Fatalf("no value arc for %v: %s", v, h)
		}
		return m[1]
	}
	// Size 200 with a 20-unit stroke: radius 89, center (100, 100).
	if got := end(50); got != "100,11" {
		t.Errorf("half-range arc ends at %s, want 100,11", got)
	}
	if got := end(100); got != "189,100" {
		t.Errorf("full-range arc ends at %s, want 189,100", got)
	}
}

// TestGaugeBandPicksTone: the arc takes the tone of the highest band the
// value has reached, whatever order the bands come in.
func TestGaugeBandPicksTone(t *testing.T) {
	bands := []ui.GaugeBand{
		{From: 90, Tone: ui.GaugeDanger},
		{From: 0, Tone: ui.GaugeSuccess},
		{From: 70, Tone: ui.GaugeWarning},
	}
	for v, want := range map[float64]string{10: "success", 70: "warning", 89.9: "warning", 95: "danger"} {
		h := string(ui.Gauge(ui.GaugeConfig{Label: "x", Value: v, Bands: bands}))
		if !classTokenPresent(h, "fui-gauge__value--"+want) {
			t.Errorf("value %v: want tone %s in %s", v, want, h)
		}
	}
	h := string(ui.Gauge(ui.GaugeConfig{Label: "x", Value: 50}))
	if !classTokenPresent(h, "fui-gauge__value--primary") {
		t.Errorf("no bands: want primary in %s", h)
	}
}

// TestGaugeNonFiniteIsEmpty: NaN or Inf is a reading that never arrived,
// so the gauge shows the calm empty state instead of a broken arc.
func TestGaugeNonFiniteIsEmpty(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		h := string(ui.Gauge(ui.GaugeConfig{Label: "CPU", Value: v}))
		if !strings.Contains(h, `data-cui-comp="ui-chart-empty"`) {
			t.Errorf("value %v: want the empty state, got %s", v, h)
		}
	}
}

// TestGaugeRefusesBadConfig: an inverted range, an unknown tone or a
// missing label is a programmer error, and panics like the other charts.
func TestGaugeRefusesBadConfig(t *testing.T) {
	for name, cfg := range map[string]ui.GaugeConfig{
		"inverted":  {Label: "x", Min: 10, Max: 5},
		"tone":      {Label: "x", Bands: []ui.GaugeBand{{From: 0, Tone: "chartreuse"}}},
		"label":     {Value: 1},
		"nan-range": {Label: "x", Max: math.NaN()},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			ui.Gauge(cfg)
		})
	}
}
