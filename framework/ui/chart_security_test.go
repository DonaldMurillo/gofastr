package ui_test

import (
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestChartConfigStringsEscaped pins the property that every config
// string concatenated into the raw chart SVG (slice/series/bar Color,
// chart ID, LabelledBy) is attribute-escaped so it cannot break out of
// its SVG attribute and inject a handler or element. Asserted across
// all three chart surfaces (Pie, Bar, Line), which share the same
// string-concatenation render path.
func TestChartConfigStringsEscaped(t *testing.T) {
	const colorAttack = `red" onload="alert(1)`
	const idAttack = `x"><script>alert(1)</script>`

	// forbidden substrings that would indicate a breakout.
	assertSafe := func(t *testing.T, out string) {
		t.Helper()
		for _, bad := range []string{
			`onload="alert(1)"`,
			`<script>alert(1)</script>`,
			`"><script`,
		} {
			if strings.Contains(out, bad) {
				t.Fatalf("attribute breakout reached SVG output: %q\n%s", bad, out)
			}
		}
	}

	t.Run("pie", func(t *testing.T) {
		h := ui.PieChart(ui.PieChartConfig{
			ID:         idAttack,
			LabelledBy: idAttack,
			Slices:     []ui.PieSlice{{Value: 1, Color: colorAttack}},
		})
		assertSafe(t, string(h))
	})

	t.Run("bar", func(t *testing.T) {
		h := ui.BarChart(ui.BarChartConfig{
			ID:         idAttack,
			LabelledBy: idAttack,
			Bars:       []ui.BarChartBar{{Label: "a", Value: 1, Color: colorAttack}},
		})
		assertSafe(t, string(h))
	})

	t.Run("line", func(t *testing.T) {
		h := ui.LineChart(ui.LineChartConfig{
			ID:         idAttack,
			LabelledBy: idAttack,
			Series:     []ui.LineSeries{{Name: "s", Values: []float64{1, 2, 3}, Color: colorAttack, Area: true}},
		})
		assertSafe(t, string(h))
	})

	t.Run("happy-path-palette-color", func(t *testing.T) {
		h := ui.PieChart(ui.PieChartConfig{
			Slices: []ui.PieSlice{{Value: 1, Color: "primary"}},
		})
		if !strings.Contains(string(h), "ui-pie-chart__slice--primary") {
			t.Fatalf("palette color class missing: %s", h)
		}
	})
}
