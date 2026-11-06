package ui

import (
	"math"
	"testing"
)

// amd64's math.Log reads a subnormal as a normal number: log10(1e-310)
// came out -307.95, so the scale jumped to 1e-308 and the tallest bar
// shrank to the 2px floor. Run under GOARCH=amd64 to see it.
func TestChartScaleHoldsForSubnormals(t *testing.T) {
	for _, v := range []float64{1e-310, 3e-315, 5e-324} {
		if got := niceCeil(v); got != 0 && (got < v || got > 10*v) {
			t.Errorf("niceCeil(%g) = %g, want a clean maximum within a decade of it", v, got)
		}
		if got := niceStep(v); got != 0 && (got < v || got > 10*v) {
			t.Errorf("niceStep(%g) = %g, want a step within a decade of it", v, got)
		}
	}
	for _, v := range []float64{1, 7, 1000, 1.2e5, 3e-300} {
		if got, want := chartLog10(v), math.Log10(v); got != want {
			t.Errorf("chartLog10(%g) = %v, math.Log10 says %v", v, got, want)
		}
	}
}
